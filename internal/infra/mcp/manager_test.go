package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

type rpcRequestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func newMCPTestHTTPServer(t *testing.T, tools []map[string]interface{}) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		var req rpcRequestEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-session")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2025-06-18",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{"listChanged": true},
					},
					"serverInfo": map[string]interface{}{
						"name":    "test-mcp",
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
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
}

func TestManagerBuildToolsManagedServerRegistersAllDiscoveredTools(t *testing.T) {
	server := newMCPTestHTTPServer(t, []map[string]interface{}{
		{
			"name":        "browser_navigate",
			"description": "navigate",
			"inputSchema": map[string]interface{}{"type": "object"},
		},
		{
			"name":        "browser_snapshot",
			"description": "snapshot",
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
				ID:            config.ManagedPlaywrightMCPServerID,
				Name:          "Playwright MCP (Managed)",
				Enabled:       true,
				Transport:     TransportHTTP,
				URL:           server.URL,
				ToolAllowlist: nil,
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(tools) != 2 {
		t.Fatalf("registered tools = %d, want 2", len(tools))
	}
}

func TestManagerBuildToolsCustomServerStillRequiresAllowlist(t *testing.T) {
	server := newMCPTestHTTPServer(t, []map[string]interface{}{
		{
			"name":        "search",
			"description": "search",
			"inputSchema": map[string]interface{}{"type": "object"},
		},
	})
	defer server.Close()

	manager := NewManager()
	tools, warnings := manager.BuildTools(context.Background(), config.MCPSettings{
		Enabled:                 true,
		VerboseLogging:          false,
		DiscoveryTimeoutSeconds: config.DefaultMCPDiscoveryTimeoutSeconds,
		ToolCallTimeoutSeconds:  config.DefaultMCPToolCallTimeoutSeconds,
		Servers: []config.MCPServerSetting{
			{
				ID:            "custom_server",
				Name:          "Custom",
				Enabled:       true,
				Transport:     TransportHTTP,
				URL:           server.URL,
				ToolAllowlist: nil,
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(tools) != 0 {
		t.Fatalf("registered tools = %d, want 0 (allowlist required for custom server)", len(tools))
	}
}

func TestShouldAutoRegisterAllDiscoveredTools(t *testing.T) {
	if !shouldAutoRegisterAllDiscoveredTools(config.ManagedPlaywrightMCPServerID) {
		t.Fatalf("expected managed server id to auto-register all discovered tools")
	}
	if shouldAutoRegisterAllDiscoveredTools("custom_server") {
		t.Fatalf("custom server should not auto-register all tools")
	}
}
