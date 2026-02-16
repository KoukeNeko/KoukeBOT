package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

func TestDecodeSettingsUpdateRequestWebToolMode(t *testing.T) {
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"web_tool_mode":"playwright_mcp"}`))
	decoded, err := decodeSettingsUpdateRequest(req)
	if err != nil {
		t.Fatalf("decodeSettingsUpdateRequest returned error: %v", err)
	}
	if decoded.WebToolMode != config.WebToolModePlaywrightMCP {
		t.Fatalf("web_tool_mode = %q, want %q", decoded.WebToolMode, config.WebToolModePlaywrightMCP)
	}
}

func TestGetSettingsResponseIncludesWebToolMode(t *testing.T) {
	dir := t.TempDir()
	store := config.NewSettingsStore(filepath.Join(dir, "settings.json"))
	if err := store.Save(config.Settings{
		WebToolMode: config.WebToolModePlaywrightMCP,
		MCP: config.MCPSettings{
			DiscoveryTimeoutSeconds: config.DefaultMCPDiscoveryTimeoutSeconds,
			ToolCallTimeoutSeconds:  config.DefaultMCPToolCallTimeoutSeconds,
		},
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	h := &Handlers{settingsStore: store}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/settings", nil)
	h.GetSettings(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp SettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WebToolMode != config.WebToolModePlaywrightMCP {
		t.Fatalf("web_tool_mode = %q, want %q", resp.WebToolMode, config.WebToolModePlaywrightMCP)
	}
}
