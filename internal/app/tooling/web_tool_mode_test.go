package tooling

import (
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

func TestApplyWebToolModeClassicKeepsSettings(t *testing.T) {
	input := config.Settings{
		WebToolMode:      config.WebToolModeClassic,
		WebSearchEnabled: true,
		WebFetchEnabled:  true,
		MCP: config.MCPSettings{
			Enabled:        false,
			VerboseLogging: false,
		},
	}

	got := ApplyWebToolMode(input)
	if got.Mode != config.WebToolModeClassic {
		t.Fatalf("mode = %q, want %q", got.Mode, config.WebToolModeClassic)
	}
	if !got.EffectiveSettings.WebSearchEnabled || !got.EffectiveSettings.WebFetchEnabled {
		t.Fatalf("classic mode should keep web_search/web_fetch enabled")
	}
	if got.EffectiveSettings.MCP.Enabled {
		t.Fatalf("classic mode should not force mcp.enabled")
	}
	if got.ForcedMCPSettings {
		t.Fatalf("classic mode should not force mcp settings")
	}
}

func TestApplyWebToolModePlaywrightForcesMCPAndManagedServer(t *testing.T) {
	input := config.Settings{
		WebToolMode:      config.WebToolModePlaywrightMCP,
		WebSearchEnabled: true,
		WebFetchEnabled:  true,
		MCP: config.MCPSettings{
			Enabled:        false,
			VerboseLogging: false,
			Servers:        nil,
		},
	}

	got := ApplyWebToolMode(input)
	if got.Mode != config.WebToolModePlaywrightMCP {
		t.Fatalf("mode = %q, want %q", got.Mode, config.WebToolModePlaywrightMCP)
	}
	if got.EffectiveSettings.WebSearchEnabled || got.EffectiveSettings.WebFetchEnabled {
		t.Fatalf("playwright mode should disable classic web tools")
	}
	if !got.EffectiveSettings.MCP.Enabled {
		t.Fatalf("playwright mode should force mcp.enabled")
	}
	if !got.EffectiveSettings.MCP.VerboseLogging {
		t.Fatalf("playwright mode should force mcp.verbose_logging")
	}
	if len(got.EffectiveSettings.MCP.Servers) != 1 {
		t.Fatalf("managed server count = %d, want 1", len(got.EffectiveSettings.MCP.Servers))
	}
	if got.EffectiveSettings.MCP.Servers[0].ID != config.ManagedPlaywrightMCPServerID {
		t.Fatalf("managed server id = %q, want %q", got.EffectiveSettings.MCP.Servers[0].ID, config.ManagedPlaywrightMCPServerID)
	}
	if !got.ManagedServerUpsert {
		t.Fatalf("expected managed server upsert=true")
	}
}

func TestEnsureManagedPlaywrightMCPServerReplacesExisting(t *testing.T) {
	servers := []config.MCPServerSetting{
		{
			ID:      config.ManagedPlaywrightMCPServerID,
			Enabled: false,
			Command: "npx",
		},
		{
			ID:      "custom",
			Enabled: true,
			Command: "custom-cmd",
		},
	}

	got, upserted := EnsureManagedPlaywrightMCPServer(servers)
	if upserted {
		t.Fatalf("expected upserted=false when managed server already exists")
	}
	if len(got) != 2 {
		t.Fatalf("server count = %d, want 2", len(got))
	}
	if got[0].ID != config.ManagedPlaywrightMCPServerID {
		t.Fatalf("managed server id = %q, want %q", got[0].ID, config.ManagedPlaywrightMCPServerID)
	}
	if got[0].Command != "playwright-mcp" {
		t.Fatalf("managed command = %q, want %q", got[0].Command, "playwright-mcp")
	}
	if !got[0].Enabled {
		t.Fatalf("managed server should be enabled")
	}
}
