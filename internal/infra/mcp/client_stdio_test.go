package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

type helperRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func TestStdioCodecCandidates(t *testing.T) {
	cases := []struct {
		name   string
		server config.MCPServerSetting
		want   []stdioCodec
	}{
		{
			name: "default framed first",
			server: config.MCPServerSetting{
				ID:      "custom",
				Command: "custom-mcp",
				Args:    []string{},
			},
			want: []stdioCodec{stdioCodecFramed, stdioCodecJSONL},
		},
		{
			name: "managed playwright id uses jsonl first",
			server: config.MCPServerSetting{
				ID:      config.ManagedPlaywrightMCPServerID,
				Command: "custom-mcp",
			},
			want: []stdioCodec{stdioCodecJSONL, stdioCodecFramed},
		},
		{
			name: "playwright package arg uses jsonl first",
			server: config.MCPServerSetting{
				ID:      "custom",
				Command: "npx",
				Args:    []string{"-y", "@playwright/mcp"},
			},
			want: []stdioCodec{stdioCodecJSONL, stdioCodecFramed},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := stdioCodecCandidates(tt.server)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("stdioCodecCandidates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunStdioListToolsWithJSONLFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	server := helperStdioServer("jsonl")
	tools, err := runStdioListTools(ctx, server, false)
	if err != nil {
		t.Fatalf("runStdioListTools error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "helper_tool" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "helper_tool")
	}
}

func TestRunStdioListToolsWithFramed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	server := helperStdioServer("framed")
	tools, err := runStdioListTools(ctx, server, false)
	if err != nil {
		t.Fatalf("runStdioListTools error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "helper_tool" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "helper_tool")
	}
}

func TestRunStdioCallToolWithJSONLFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	server := helperStdioServer("jsonl")
	content, isError, err := runStdioCallTool(ctx, server, "helper_tool", map[string]interface{}{
		"foo": "bar",
	}, false)
	if err != nil {
		t.Fatalf("runStdioCallTool error: %v", err)
	}
	if isError {
		t.Fatalf("isError = true, want false")
	}
	if !strings.Contains(content, "helper call ok") {
		t.Fatalf("content = %q, want to contain helper call ok", content)
	}
}

func TestRunStdioListToolsIncludesStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	server := helperStdioServer("stderr_exit")
	_, err := runStdioListTools(ctx, server, false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "helper stderr boom") {
		t.Fatalf("error = %v, want stderr summary", err)
	}
}

func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := helperModeFromArgs()
	switch mode {
	case "jsonl":
		runJSONLHelperProcess()
	case "framed":
		runFramedHelperProcess()
	case "stderr_exit":
		fmt.Fprintln(os.Stderr, "helper stderr boom")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode: %q\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func helperModeFromArgs() string {
	for idx, arg := range os.Args {
		if arg == "--" && idx+1 < len(os.Args) {
			return os.Args[idx+1]
		}
	}
	return ""
}

func helperStdioServer(mode string) config.MCPServerSetting {
	return config.MCPServerSetting{
		ID:        "helper_server",
		Name:      "helper",
		Enabled:   true,
		Transport: TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestMCPStdioHelperProcess", "--", mode},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
		},
	}
}

func runJSONLHelperProcess() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := dispatchHelperRequest(stdioCodecJSONL, []byte(line)); err != nil {
			fmt.Fprintf(os.Stderr, "jsonl helper error: %v\n", err)
			os.Exit(1)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "jsonl scanner error: %v\n", err)
		os.Exit(1)
	}
}

func runFramedHelperProcess() {
	reader := bufio.NewReader(os.Stdin)
	for {
		frame, err := readFramedJSON(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			fmt.Fprintf(os.Stderr, "framed helper read error: %v\n", err)
			os.Exit(1)
		}
		if err := dispatchHelperRequest(stdioCodecFramed, frame); err != nil {
			fmt.Fprintf(os.Stderr, "framed helper dispatch error: %v\n", err)
			os.Exit(1)
		}
	}
}

func dispatchHelperRequest(codec stdioCodec, frame []byte) error {
	var req helperRPCRequest
	if err := json.Unmarshal(frame, &req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}

	switch req.Method {
	case "initialize":
		return writeHelperResponse(codec, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "helper-mcp",
					"version": "0.1.0",
				},
			},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		return writeHelperResponse(codec, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "helper_tool",
						"description": "helper test tool",
						"inputSchema": map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
		})
	case "tools/call":
		return writeHelperResponse(codec, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"isError": false,
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "helper call ok",
					},
				},
			},
		})
	default:
		return writeHelperResponse(codec, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": "method not found",
			},
		})
	}
}

func writeHelperResponse(codec stdioCodec, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	switch codec {
	case stdioCodecJSONL:
		return writeJSONLine(os.Stdout, body)
	case stdioCodecFramed:
		return writeFramedJSON(os.Stdout, body)
	default:
		return fmt.Errorf("unsupported helper codec: %s", codec)
	}
}
