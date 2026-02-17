package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/app/tooling"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const personaConfigsBaseDir = "configs/personas"

type SettingsRuntimeApplier struct {
	handlers *Handlers
}

func NewSettingsRuntimeApplier(h *Handlers) *SettingsRuntimeApplier {
	return &SettingsRuntimeApplier{handlers: h}
}

func (a *SettingsRuntimeApplier) SaveAndApply(existing config.Settings) (map[string]string, error) {
	h := a.handlers
	if h == nil {
		return nil, fmt.Errorf("handlers are not configured")
	}

	previous, err := h.settingsStore.Load()
	if err != nil {
		return nil, fmt.Errorf("load current settings: %w", err)
	}

	if config.NormalizeWebToolMode(existing.WebToolMode) == config.WebToolModePlaywrightMCP {
		nextServers, upserted := tooling.EnsureManagedPlaywrightMCPServer(existing.MCP.Servers)
		existing.MCP.Servers = nextServers
		if upserted {
			logAPI.Logf("settings: auto-provisioned managed MCP server id=%s", config.ManagedPlaywrightMCPServerID)
		}
	}

	providerRelatedChanged := hasProviderRelatedSettingsChange(previous, existing)
	var builtProvider llm.Provider
	if providerRelatedChanged {
		strictOAuthProbe := shouldRunStrictOAuthProbe(existing)
		logAPI.Logf("settings: provider-related fields changed, pre-validating provider (strict_oauth_probe=%v)", strictOAuthProbe)
		builtProvider, err = h.buildProviderForSettings(existing, strictOAuthProbe)
		if err != nil {
			return nil, newSettingsClientError("provider validation failed: %v", err)
		}
	}

	logAPI.Logf("settings: saving model=%s provider=%s mode=%s websearch=%v reply=%v provider_related_changed=%v",
		existing.GeminiModel, existing.ProviderType, existing.WebToolMode, existing.WebSearchEnabled, existing.DiscordUseReply, providerRelatedChanged)

	if err := h.settingsStore.Save(existing); err != nil {
		return nil, fmt.Errorf("save settings: %w", err)
	}

	if h.discordPersona != nil && existing.DiscordPersonaID != "" {
		h.discordPersona.SetPersonaID(existing.DiscordPersonaID)
		logAPI.Logf("settings: discord persona switched to %s", existing.DiscordPersonaID)
	}

	h.chatService.UpdatePromptGuardPolicy(app.PromptGuardPolicy{
		PersonaLockEveryTurns:     existing.PersonaLockEveryTurns,
		TripletOutputEnabled:      existing.TripletOutputEnabled,
		AnalysisVisibility:        existing.AnalysisVisibility,
		NegativeConstraintEnabled: existing.NegativeConstraintEnabled,
		NegativeConstraintDefault: existing.NegativeConstraintDefault,
	})
	h.chatService.UpdateGlobalTimezone(existing.Timezone)
	h.chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		ToolGuardEnabled:               existing.ToolGuardEnabled,
		MaxToolRounds:                  existing.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       existing.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: existing.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  existing.DiscordShowInternalToolErrors,
	})
	h.chatService.UpdateMemoryRetentionPolicy(app.MemoryRetentionPolicy{
		RecentRounds: existing.MemoryRecentRounds,
	})
	if h.schedulerService != nil {
		h.schedulerService.UpdatePolicy(schedulerpkg.Policy{
			MonitorMaxPerUser: existing.ScheduledMonitorMaxPerUser,
		})
	}
	if pool := h.providerManager.GetPool(); pool != nil {
		pool.SetTransientRecoveryWait(
			existing.TransientRecoveryWaitEnabled,
			time.Duration(existing.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
		if existing.FallbackModelEnabled && existing.FallbackModel != "" {
			pool.SetFallbackModel(existing.FallbackModel)
		} else {
			pool.SetFallbackModel("")
		}
	}

	fileToolPersonaID := tooling.SelectFileToolPersonaID(personaConfigsBaseDir, existing.DiscordPersonaID)
	catalog := tooling.BuildCatalog(h.ctx, existing, fileToolPersonaID, h.discordRuntime, h.schedulerService, h.mcpManager)
	h.providerManager.RegisterTools(catalog.Tools)
	h.chatService.UpdateToolRegistry(catalog.Tools)
	for _, warning := range catalog.Warnings {
		logAPI.Warnf("settings: tool catalog warning: %s", warning)
	}

	response := map[string]string{"status": "saved"}
	if providerRelatedChanged {
		if builtProvider == nil {
			return nil, fmt.Errorf("provider validation returned nil provider")
		}
		h.providerManager.Update(builtProvider)
		info := builtProvider.ModelInfo()
		logAPI.Logf("provider hot-swapped: %s (model: %s)", info.ProviderName, info.Model)
	}
	if len(catalog.Warnings) > 0 {
		warningText := fmt.Sprintf("MCP warnings: %s", strings.Join(catalog.Warnings, "; "))
		if existingWarning := response["warning"]; existingWarning != "" {
			response["warning"] = existingWarning + " | " + warningText
		} else {
			response["warning"] = warningText
		}
	}

	logAPI.Logf("settings: saved (warning=%v)", response["warning"] != "")
	return response, nil
}

func hasProviderRelatedSettingsChange(previous, next config.Settings) bool {
	if normalizeProviderTypeForSelection(previous.ProviderType) != normalizeProviderTypeForSelection(next.ProviderType) {
		return true
	}
	if llm.NormalizeModelID(previous.GeminiModel) != llm.NormalizeModelID(next.GeminiModel) {
		return true
	}
	if previous.GeminiAPIKey != next.GeminiAPIKey {
		return true
	}
	if previous.FallbackModelEnabled != next.FallbackModelEnabled {
		return true
	}
	return llm.NormalizeModelID(previous.FallbackModel) != llm.NormalizeModelID(next.FallbackModel)
}

func shouldRunStrictOAuthProbe(settings config.Settings) bool {
	switch normalizeProviderTypeForSelection(settings.ProviderType) {
	case config.ProviderOAuth:
		return true
	case config.ProviderAPIKey:
		return false
	default:
		// Auto mode falls back to OAuth when no API key exists.
		return strings.TrimSpace(settings.GeminiAPIKey) == ""
	}
}
