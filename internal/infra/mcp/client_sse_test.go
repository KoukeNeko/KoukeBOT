package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

// sseTestServer simulates a Legacy SSE MCP server:
//   - GET /sse: persistent SSE stream, first sends "endpoint", then RPC responses
//   - POST /messages/: receives JSON-RPC, responds 202, pushes response to the SSE stream
type sseTestServer struct {
	tools []map[string]interface{}

	mu       sync.Mutex
	// Each SSE client gets its own response channel keyed by session ID.
	sessions map[string]chan []byte
}

func newSSETestServer(tools []map[string]interface{}) *sseTestServer {
	return &sseTestServer{
		tools:    tools,
		sessions: make(map[string]chan []byte),
	}
}

func (s *sseTestServer) getResponseChan(sessionID string) chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.sessions[sessionID]
	if !ok {
		ch = make(chan []byte, 32)
		s.sessions[sessionID] = ch
	}
	return ch
}

func (s *sseTestServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sse":
			s.handleSSEStream(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/messages/"):
			s.handleMessage(w, r)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
}

func (s *sseTestServer) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sessionID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	responseChan := s.getResponseChan(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	// Send the endpoint event.
	fmt.Fprintf(w, "event: endpoint\ndata: /messages/?session_id=%s\n\n", sessionID)
	flusher.Flush()

	// Forward responses from the channel to the SSE stream.
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-responseChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(data))
			flusher.Flush()
		}
	}
}

func (s *sseTestServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	sessionID := r.URL.Query().Get("session_id")
	responseChan := s.getResponseChan(sessionID)

	var req rpcRequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Accept the request with 202.
	w.WriteHeader(http.StatusAccepted)

	// Build the JSON-RPC response and push it to the SSE stream.
	var result interface{}
	switch req.Method {
	case "initialize":
		result = map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{"listChanged": true},
			},
			"serverInfo": map[string]interface{}{
				"name":    "test-sse-mcp",
				"version": "0.1.0",
			},
		}
	case "notifications/initialized":
		// No response for notifications.
		return
	case "tools/list":
		result = map[string]interface{}{
			"tools": s.tools,
		}
	case "tools/call":
		result = map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "tool result ok"},
			},
		}
	default:
		errResp, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]interface{}{"code": -32601, "message": "method not found"},
		})
		responseChan <- errResp
		return
	}

	resp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result":  result,
	})
	responseChan <- resp
}

func newMCPTestSSEServer(t *testing.T, tools []map[string]interface{}) *httptest.Server {
	t.Helper()
	srv := newSSETestServer(tools)
	return httptest.NewServer(srv.handler())
}

func TestSSEConnectSuccess(t *testing.T) {
	server := newMCPTestSSEServer(t, nil)
	defer server.Close()

	session := newSSESession(http.DefaultClient, config.MCPServerSetting{
		ID:  "test-sse",
		URL: server.URL + "/sse",
	}, false)
	defer session.close()

	if err := session.connect(context.Background()); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if !strings.HasPrefix(session.messageURL, server.URL+"/messages/") {
		t.Fatalf("messageURL = %q, want prefix %q", session.messageURL, server.URL+"/messages/")
	}
}

func TestSSEConnectNoEndpointEvent(t *testing.T) {
	// Server that returns SSE but no "endpoint" event, then closes.
	noEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: ping\ndata: keepalive\n\n")
		// Close immediately — no endpoint event.
	}))
	defer noEndpoint.Close()

	session := newSSESession(http.DefaultClient, config.MCPServerSetting{
		ID:  "test-no-endpoint",
		URL: noEndpoint.URL + "/sse",
	}, false)
	defer session.close()

	err := session.connect(context.Background())
	if err == nil {
		t.Fatal("expected connect error, got nil")
	}
	if !strings.Contains(err.Error(), "no endpoint event") {
		t.Fatalf("expected 'no endpoint event' error, got: %v", err)
	}
}

func TestSSEResolveEndpointURL(t *testing.T) {
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
