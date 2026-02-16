package tooling

import (
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logMCPMode = logger.New("MCP", logger.Cyan)

type WebToolModeResult struct {
	Mode                 string
	EffectiveSettings    config.Settings
	ManagedServerUpsert  bool
	ForcedMCPSettings    bool
	DisabledClassicTools bool
}

func ApplyWebToolMode(settings config.Settings) WebToolModeResult {
	mode := config.NormalizeWebToolMode(settings.WebToolMode)
	effective := settings
	effective.WebToolMode = mode

	result := WebToolModeResult{
		Mode:              mode,
		EffectiveSettings: effective,
	}

	if mode != config.WebToolModePlaywrightMCP {
		return result
	}

	result.DisabledClassicTools = effective.WebSearchEnabled || effective.WebFetchEnabled
	effective.WebSearchEnabled = false
	effective.WebFetchEnabled = false

	result.ForcedMCPSettings = !effective.MCP.Enabled || !effective.MCP.VerboseLogging
	effective.MCP.Enabled = true
	effective.MCP.VerboseLogging = true

	var upserted bool
	effective.MCP.Servers, upserted = EnsureManagedPlaywrightMCPServer(effective.MCP.Servers)
	result.ManagedServerUpsert = upserted
	result.EffectiveSettings = effective

	logMCPMode.Logf(
		"mode.apply mode=%s force_mcp=%t disable_classic=%t managed_upsert=%t",
		mode,
		result.ForcedMCPSettings,
		result.DisabledClassicTools,
		result.ManagedServerUpsert,
	)

	return result
}

func EnsureManagedPlaywrightMCPServer(servers []config.MCPServerSetting) ([]config.MCPServerSetting, bool) {
	managed := managedPlaywrightServerSetting()
	if len(servers) == 0 {
		return []config.MCPServerSetting{managed}, true
	}

	next := make([]config.MCPServerSetting, 0, len(servers)+1)
	found := false
	for _, server := range servers {
		if !isManagedPlaywrightServerID(server.ID) {
			next = append(next, server)
			continue
		}
		if found {
			continue
		}
		next = append(next, managed)
		found = true
	}

	if !found {
		next = append(next, managed)
		return next, true
	}
	return next, false
}

func isManagedPlaywrightServerID(id string) bool {
	return strings.TrimSpace(id) == config.ManagedPlaywrightMCPServerID
}

func managedPlaywrightServerSetting() config.MCPServerSetting {
	return config.MCPServerSetting{
		ID:        config.ManagedPlaywrightMCPServerID,
		Name:      "Playwright MCP (Managed)",
		Enabled:   true,
		Transport: "stdio",
		Command:   "playwright-mcp",
		Args:      []string{"--headless", "--no-sandbox", "--isolated"},
	}
}
