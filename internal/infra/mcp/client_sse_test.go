package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

// newMCPTestSSEServer creates a mock MCP server that implements the Legacy SSE
// transport: GET /sse returns an SSE endpoint event, POST /messages/ handles
// JSON-RPC requests.
func newMCPTestSSEServer(t *testing.T, tools []map[string]interface{}) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "event: endpoint\ndata: /messages/?session_id=test-session\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/messages/"):
			defer r.Body.Close()
			var req rpcRequestEnvelope
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			switch req.Method {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"protocolVersion": "2025-06-18",
						"capabilities": map[string]interface{}{
							"tools": map[string]interface{}{"listChanged": true},
						},
						"serverInfo": map[string]interface{}{
							"name":    "test-sse-mcp",
							"version": "0.1.0",
						},
					},
				})
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
			case "tools/list":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"tools": tools,
					},
				})
			case "tools/call":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"content": []map[string]interface{}{
							{"type": "text", "text": "tool result ok"},
						},
					},
				})
			default:
				http.Error(w, "unknown method", http.StatusBadRequest)
			}

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func TestSSEHandshakeSuccess(t *testing.T) {
	server := newMCPTestSSEServer(t, nil)
	defer server.Close()

	session := newSSESession(http.DefaultClient, config.MCPServerSetting{
		ID:  "test-sse",
		URL: server.URL + "/sse",
	}, false)

	if err := session.handshake(context.Background()); err != nil {
		t.Fatalf("handshake failed: %v", err)
	}

	expected := server.URL + "/messages/?session_id=test-session"
	if session.messageURL != expected {
		t.Fatalf("messageURL = %q, want %q", session.messageURL, expected)
	}
}

func TestSSEHandshakeNoEndpointEvent(t *testing.T) {
	// Server that returns SSE but no "endpoint" event.
	noEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: ping\ndata: keepalive\n\n")
	}))
	defer noEndpoint.Close()

	session := newSSESession(http.DefaultClient, config.MCPServerSetting{
		ID:  "test-no-endpoint",
		URL: noEndpoint.URL + "/sse",
	}, false)

	err := session.handshake(context.Background())
	if err == nil {
		t.Fatal("expected handshake error, got nil")
	}
	if !strings.Contains(err.Error(), "no endpoint event") {
		t.Fatalf("expected 'no endpoint event' error, got: %v", err)
	}
}

func TestSSEHandshakeRelativePathResolution(t *testing.T) {
	tests := []struct {
		baseURL      string
		endpointPath string
		wantURL      string
	}{
		{
			baseURL:      "http://localhost:8100/sse",
			endpointPath: "/messages/?session_id=abc",
			wantURL:      "http://localhost:8100/messages/?session_id=abc",
		},
		{
			baseURL:      "http://192.168.1.1:9000/api/sse",
			endpointPath: "/api/messages/?session_id=xyz",
			wantURL:      "http://192.168.1.1:9000/api/messages/?session_id=xyz",
		},
		{
			baseURL:      "https://mcp.example.com/sse",
			endpointPath: "https://mcp.example.com/messages/?session_id=full",
			wantURL:      "https://mcp.example.com/messages/?session_id=full",
		},
	}

	for _, tt := range tests {
		resolved, err := resolveEndpointURL(tt.baseURL, tt.endpointPath)
		if err != nil {
			t.Errorf("resolveEndpointURL(%q, %q) error: %v", tt.baseURL, tt.endpointPath, err)
			continue
		}
		if resolved != tt.wantURL {
			t.Errorf("resolveEndpointURL(%q, %q) = %q, want %q", tt.baseURL, tt.endpointPath, resolved, tt.wantURL)
		}
	}
}

func TestRunSSEListTools(t *testing.T) {
	server := newMCPTestSSEServer(t, []map[string]interface{}{
		{
			"name":        "generate_image",
			"description": "generate an image via stable diffusion",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string"},
			}},
		},
		{
			"name":        "list_models",
			"description": "list available models",
			"inputSchema": map[string]interface{}{"type": "object"},
		},
	})
	defer server.Close()

	tools, err := runSSEListTools(context.Background(), http.DefaultClient, config.MCPServerSetting{
		ID:      "sd-server",
		URL:     server.URL + "/sse",
		Enabled: true,
	}, false)
	if err != nil {
		t.Fatalf("runSSEListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("discovered tools = %d, want 2", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["generate_image"] {
		t.Error("missing tool: generate_image")
	}
	if !names["list_models"] {
		t.Error("missing tool: list_models")
	}
}

func TestRunSSECallTool(t *testing.T) {
	server := newMCPTestSSEServer(t, nil)
	defer server.Close()

	content, isError, err := runSSECallTool(
		context.Background(),
		http.DefaultClient,
		config.MCPServerSetting{
			ID:      "sd-server",
			URL:     server.URL + "/sse",
			Enabled: true,
		},
		"generate_image",
		map[string]interface{}{"prompt": "a cat"},
		false,
	)
	if err != nil {
		t.Fatalf("runSSECallTool failed: %v", err)
	}
	if isError {
		t.Fatal("expected isError=false")
	}
	if !strings.Contains(content, "tool result ok") {
		t.Fatalf("content = %q, want to contain 'tool result ok'", content)
	}
}

func TestManagerBuildToolsWithSSETransport(t *testing.T) {
	server := newMCPTestSSEServer(t, []map[string]interface{}{
		{
			"name":        "sd_generate",
			"description": "generate image",
			"inputSchema": map[string]interface{}{"type": "object"},
		},
	})
	defer server.Close()

	manager := NewManager()
	tools, warnings := manager.BuildTools(context.Background(), config.MCPSettings{
		Enabled:                 true,
		VerboseLogging:          true,
		DiscoveryTimeoutSeconds: config.DefaultMCPDiscoveryTimeoutSeconds,
		ToolCallTimeoutSeconds:  config.DefaultMCPToolCallTimeoutSeconds,
		Servers: []config.MCPServerSetting{
			{
				ID:            "sse-sd",
				Name:          "Stable Diffusion SSE",
				Enabled:       true,
				Transport:     TransportSSE,
				URL:           server.URL + "/sse",
				ToolAllowlist: []string{"sd_generate"},
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(tools) != 1 {
		t.Fatalf("registered tools = %d, want 1", len(tools))
	}
}
