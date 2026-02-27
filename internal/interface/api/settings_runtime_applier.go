package api

import (
	"fmt"
	"os"
	"strings"

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
	existing = backfillProviderFieldsFromPrevious(previous, existing)

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
		existing.ModelID, existing.ProviderID, existing.WebToolMode, existing.WebSearchEnabled, existing.DiscordUseReply, providerRelatedChanged)

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
		configureOAuthPoolFromSettings(pool, existing, existing.ProviderID, h.oauthFlow.PoolPath())
	}

	fileToolPersonaID := tooling.SelectFileToolPersonaID(personaConfigsBaseDir, existing.DiscordPersonaID)
	catalog := tooling.BuildCatalog(h.ctx, existing, fileToolPersonaID, h.discordRuntime, h.schedulerService, h.mcpManager)
	h.providerManager.RegisterTools(catalog.Tools)
	h.chatService.UpdateToolRegistry(catalog.Tools)
	h.chatService.UpdateMCPInstructions(catalog.MCPInstructions)
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

func backfillProviderFieldsFromPrevious(previous, next config.Settings) config.Settings {
	if hasExplicitProviderPayload(next) {
		return next
	}
	next.ProviderType = previous.ProviderType
	next.ProviderID = previous.ProviderID
	next.ModelID = previous.ModelID
	next.GeminiModel = previous.GeminiModel
	next.GeminiAPIKey = previous.GeminiAPIKey
	next.FallbackEnabled = previous.FallbackEnabled
	next.FallbackModelEnabled = previous.FallbackModelEnabled
	next.FallbackModelID = previous.FallbackModelID
	next.FallbackModel = previous.FallbackModel
	next.FallbackSecondModelID = previous.FallbackSecondModelID
	next.FallbackSecondModel = previous.FallbackSecondModel
	next.FallbackThirdModelID = previous.FallbackThirdModelID
	next.FallbackThirdModel = previous.FallbackThirdModel
	next.EndpointPolicy = previous.EndpointPolicy
	return next
}

func hasExplicitProviderPayload(settings config.Settings) bool {
	return strings.TrimSpace(settings.ProviderType) != "" ||
		strings.TrimSpace(settings.ProviderID) != "" ||
		strings.TrimSpace(settings.ModelID) != "" ||
		strings.TrimSpace(settings.GeminiModel) != "" ||
		strings.TrimSpace(settings.GeminiAPIKey) != "" ||
		strings.TrimSpace(settings.FallbackModelID) != "" ||
		strings.TrimSpace(settings.FallbackModel) != "" ||
		strings.TrimSpace(settings.FallbackSecondModelID) != "" ||
		strings.TrimSpace(settings.FallbackSecondModel) != "" ||
		strings.TrimSpace(settings.FallbackThirdModelID) != "" ||
		strings.TrimSpace(settings.FallbackThirdModel) != "" ||
		settings.FallbackEnabled ||
		settings.FallbackModelEnabled ||
		strings.TrimSpace(settings.EndpointPolicy) != ""
}

func hasProviderRelatedSettingsChange(previous, next config.Settings) bool {
	prevProviderID, prevModel, prevModelID := llm.ResolveProviderAndModel(previous.ProviderID, previous.ModelID, previous.GeminiModel)
	nextProviderID, nextModel, nextModelID := llm.ResolveProviderAndModel(next.ProviderID, next.ModelID, next.GeminiModel)

	if prevProviderID != nextProviderID {
		return true
	}
	if prevModelID != nextModelID {
		return true
	}
	if llm.NormalizeGoogleModelAlias(prevModel) != llm.NormalizeGoogleModelAlias(nextModel) {
		return true
	}
	if normalizeProviderTypeForSelection(previous.ProviderType) != normalizeProviderTypeForSelection(next.ProviderType) {
		return true
	}
	if previous.GeminiAPIKey != next.GeminiAPIKey {
		return true
	}
	// Fallback model and endpoint policy changes do NOT require provider re-validation:
	// they are applied directly to the runtime pool via configureOAuthPoolFromSettings.
	// Including them here would trigger an unnecessary OAuth probe (and block the save
	// during MODEL_CAPACITY cooldown periods when all accounts are rate-limited).
	return llm.ResolveEndpointPolicy(previous.EndpointPolicy) != llm.ResolveEndpointPolicy(next.EndpointPolicy)
}

func shouldRunStrictOAuthProbe(settings config.Settings) bool {
	if strings.TrimSpace(settings.ProviderID) != "" {
		switch llm.NormalizeGoogleProviderID(settings.ProviderID) {
		case llm.ProviderGoogleAntigravity:
			return true
		case llm.ProviderGoogleGeminiCLI:
			// Probe when using OAuth pool; skip when using local CLI binary.
			return normalizeProviderTypeForSelection(settings.ProviderType) == config.ProviderOAuth
		case llm.ProviderAIStudioRPC:
			// AI Studio direct provider does not use OAuth pool probes.
			return false
		case llm.ProviderOpenAIAPIKey:
			return false
		case llm.ProviderGeminiAPIKey:
			return false
		}
	}

	switch normalizeProviderTypeForSelection(settings.ProviderType) {
	case config.ProviderOAuth:
		return true
	case config.ProviderAPIKey:
		return false
	default:
		// Auto mode falls back to OAuth when no API key exists.
		return strings.TrimSpace(settings.GeminiAPIKey) == "" &&
			strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == ""
	}
}
