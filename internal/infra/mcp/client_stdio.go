package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	codec     stdioCodec
	verbose   bool
}

func newStdioSession(ctx context.Context, server config.MCPServerSetting, codec stdioCodec, verbose bool) (*stdioSession, error) {
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
		codec:    codec,
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

func (s *stdioSession) stderrSummary() string {
	if s == nil {
		return ""
	}
	stderr := strings.TrimSpace(s.stderrBuf.String())
	if stderr == "" {
		return ""
	}
	const maxLen = 480
	if len(stderr) <= maxLen {
		return stderr
	}
	return stderr[:maxLen-3] + "..."
}

func (s *stdioSession) writeRPC(payload []byte) error {
	switch s.codec {
	case stdioCodecJSONL:
		return writeJSONLine(s.stdin, payload)
	case stdioCodecFramed:
		return writeFramedJSON(s.stdin, payload)
	default:
		return fmt.Errorf("unsupported stdio codec: %s", s.codec)
	}
}

func (s *stdioSession) readRPC() ([]byte, error) {
	switch s.codec {
	case stdioCodecJSONL:
		return readJSONLine(s.stdout)
	case stdioCodecFramed:
		return readFramedJSON(s.stdout)
	default:
		return nil, fmt.Errorf("unsupported stdio codec: %s", s.codec)
	}
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
	if err := s.writeRPC(body); err != nil {
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

		frame, err := s.readRPC()
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
	if err := s.writeRPC(body); err != nil {
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
		"%s server=%s transport=stdio codec=%s rpc_method=%s rpc_id=%v payload=%s",
		tag,
		s.serverID,
		s.codec,
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
		"stdio.res server=%s transport=stdio codec=%s rpc_method=%s rpc_id=%v matched=%t payload=%s",
		s.serverID,
		s.codec,
		rpcMethod,
		requestID,
		matched,
		string(sanitizeJSONBytes(frame)),
	)
}

func runStdioListTools(ctx context.Context, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	codecs := stdioCodecCandidates(server)
	attemptErrs := make([]error, 0, len(codecs))
	for _, codec := range codecs {
		tools, err := runStdioListToolsWithCodec(ctx, server, codec, verbose)
		if err == nil {
			return tools, nil
		}
		attemptErrs = append(attemptErrs, fmt.Errorf("codec=%s: %w", codec, err))
		if ctx.Err() != nil {
			break
		}
	}

	if len(attemptErrs) == 1 {
		return nil, attemptErrs[0]
	}
	return nil, fmt.Errorf("stdio discovery failed: %w", errors.Join(attemptErrs...))
}

func runStdioCallTool(ctx context.Context, server config.MCPServerSetting, toolName string, input map[string]interface{}, verbose bool) (string, bool, error) {
	codecs := stdioCodecCandidates(server)
	attemptErrs := make([]error, 0, len(codecs))
	for _, codec := range codecs {
		content, isError, err := runStdioCallToolWithCodec(ctx, server, toolName, input, codec, verbose)
		if err == nil {
			return content, isError, nil
		}
		attemptErrs = append(attemptErrs, fmt.Errorf("codec=%s: %w", codec, err))
		if ctx.Err() != nil {
			break
		}
	}

	if len(attemptErrs) == 1 {
		return "", false, attemptErrs[0]
	}
	return "", false, fmt.Errorf("stdio tool call failed: %w", errors.Join(attemptErrs...))
}

func runStdioListToolsWithCodec(ctx context.Context, server config.MCPServerSetting, codec stdioCodec, verbose bool) (tools []ToolDescriptor, err error) {
	session, err := newStdioSession(ctx, server, codec, verbose)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = withSessionCloseError(err, session.close())
		err = withSessionStderr(err, session.stderrSummary())
	}()

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
	tools, err = parseToolsListResult(result)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func runStdioCallToolWithCodec(
	ctx context.Context,
	server config.MCPServerSetting,
	toolName string,
	input map[string]interface{},
	codec stdioCodec,
	verbose bool,
) (content string, isError bool, err error) {
	session, err := newStdioSession(ctx, server, codec, verbose)
	if err != nil {
		return "", false, err
	}
	defer func() {
		err = withSessionCloseError(err, session.close())
		err = withSessionStderr(err, session.stderrSummary())
	}()

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

	content, isError, err = parseToolCallResponse(result)
	if err != nil {
		return "", false, err
	}
	return content, isError, nil
}

func withSessionCloseError(base error, closeErr error) error {
	if closeErr == nil {
		return base
	}
	if base == nil {
		return closeErr
	}
	return errors.Join(base, closeErr)
}

func withSessionStderr(base error, stderr string) error {
	if base == nil {
		return nil
	}
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return base
	}
	if strings.Contains(base.Error(), "stderr:") {
		return base
	}
	return errors.Join(base, fmt.Errorf("stderr: %s", stderr))
}

func stdioCodecCandidates(server config.MCPServerSetting) []stdioCodec {
	if prefersJSONLStdio(server) {
		return []stdioCodec{stdioCodecJSONL, stdioCodecFramed}
	}
	return []stdioCodec{stdioCodecFramed, stdioCodecJSONL}
}

func prefersJSONLStdio(server config.MCPServerSetting) bool {
	if strings.TrimSpace(server.ID) == config.ManagedPlaywrightMCPServerID {
		return true
	}

	targets := make([]string, 0, len(server.Args)+1)
	targets = append(targets, server.Command)
	targets = append(targets, server.Args...)
	for _, target := range targets {
		token := strings.ToLower(strings.TrimSpace(target))
		if strings.Contains(token, "playwright-mcp") {
			return true
		}
		if strings.Contains(token, "@playwright/mcp") {
			return true
		}
	}
	return false
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
