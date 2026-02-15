package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

type stdioSession struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	stderrBuf bytes.Buffer
	nextID    int64
	serverID  string
	verbose   bool
}

func newStdioSession(ctx context.Context, server config.MCPServerSetting, verbose bool) (*stdioSession, error) {
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Env = append(os.Environ(), buildEnvList(server.Env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open stderr: %w", err)
	}

	s := &stdioSession{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdoutPipe),
		serverID: server.ID,
		verbose:  verbose,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	go func() {
		_, _ = io.Copy(&s.stderrBuf, stderrPipe)
	}()

	return s, nil
}

func (s *stdioSession) close() error {
	if s == nil {
		return nil
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd == nil {
		return nil
	}
	if err := s.cmd.Wait(); err != nil {
		stderr := strings.TrimSpace(s.stderrBuf.String())
		if stderr == "" {
			return err
		}
		return fmt.Errorf("%w (stderr: %s)", err, stderr)
	}
	return nil
}

func (s *stdioSession) call(ctx context.Context, method string, params interface{}, expectResponse bool) (json.RawMessage, error) {
	id := atomic.AddInt64(&s.nextID, 1)
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if err := writeFramedJSON(s.stdin, body); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	s.logRequest("stdio.req", method, request.ID, body)

	if !expectResponse {
		return nil, nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		frame, err := readFramedJSON(s.stdout)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		var response rpcResponse
		if err := json.Unmarshal(frame, &response); err != nil {
			s.logFrame(method, request.ID, frame, false)
			continue
		}
		matched := fmt.Sprint(response.ID) == fmt.Sprint(id)
		s.logFrame(method, request.ID, frame, matched)
		if !matched {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("%s", rpcErrorMessage(response))
		}
		return response.Result, nil
	}
}

func (s *stdioSession) notify(method string, params interface{}) error {
	request := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	if err := writeFramedJSON(s.stdin, body); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	s.logRequest("stdio.notify", method, nil, body)
	return nil
}

func (s *stdioSession) logRequest(tag, rpcMethod string, requestID interface{}, payload []byte) {
	if !s.verbose {
		return
	}
	logMCP.Logf(
		"%s server=%s transport=stdio rpc_method=%s rpc_id=%v payload=%s",
		tag,
		s.serverID,
		rpcMethod,
		requestID,
		string(sanitizeJSONBytes(payload)),
	)
}

func (s *stdioSession) logFrame(rpcMethod string, requestID interface{}, frame []byte, matched bool) {
	if !s.verbose {
		return
	}
	logMCP.Logf(
		"stdio.res server=%s transport=stdio rpc_method=%s rpc_id=%v matched=%t payload=%s",
		s.serverID,
		rpcMethod,
		requestID,
		matched,
		string(sanitizeJSONBytes(frame)),
	)
}

func runStdioListTools(ctx context.Context, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	session, err := newStdioSession(ctx, server, verbose)
	if err != nil {
		return nil, err
	}
	defer session.close()

	if _, err := session.call(ctx, "initialize", newInitializeParams(), true); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}
	if err := session.notify("notifications/initialized", map[string]interface{}{}); err != nil {
		return nil, fmt.Errorf("initialized notification failed: %w", err)
	}
	result, err := session.call(ctx, "tools/list", map[string]interface{}{}, true)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}
	tools, err := parseToolsListResult(result)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func runStdioCallTool(ctx context.Context, server config.MCPServerSetting, toolName string, input map[string]interface{}, verbose bool) (string, bool, error) {
	session, err := newStdioSession(ctx, server, verbose)
	if err != nil {
		return "", false, err
	}
	defer session.close()

	if _, err := session.call(ctx, "initialize", newInitializeParams(), true); err != nil {
		return "", false, fmt.Errorf("initialize failed: %w", err)
	}
	if err := session.notify("notifications/initialized", map[string]interface{}{}); err != nil {
		return "", false, fmt.Errorf("initialized notification failed: %w", err)
	}

	result, err := session.call(ctx, "tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": input,
	}, true)
	if err != nil {
		return "", false, fmt.Errorf("tools/call failed: %w", err)
	}

	content, isError, err := parseToolCallResponse(result)
	if err != nil {
		return "", false, err
	}
	return content, isError, nil
}

func buildEnvList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	values := make([]string, 0, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values = append(values, key+"="+value)
	}
	return values
}
