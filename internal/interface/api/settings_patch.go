package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func decodeSettingsUpdateRequest(r *http.Request) (SettingsUpdateRequest, error) {
	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return SettingsUpdateRequest{}, err
	}
	return req, nil
}

func (h *Handlers) applySettingsPatch(existing *config.Settings, req SettingsUpdateRequest) error {
	if existing == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	if req.GeminiAPIKey != "" && !isMasked(req.GeminiAPIKey) {
		existing.GeminiAPIKey = req.GeminiAPIKey
	}
	if req.GeminiModel != "" {
		existing.GeminiModel = llm.NormalizeGoogleModelAlias(req.GeminiModel)
	}
	if req.DiscordToken != "" && !isMasked(req.DiscordToken) {
		existing.DiscordToken = req.DiscordToken
	}
	if req.ProviderType != "" {
		existing.ProviderType = req.ProviderType
		switch normalizeProviderTypeForSelection(req.ProviderType) {
		case config.ProviderAPIKey:
			if req.ProviderID == "" {
				existing.ProviderID = llm.ProviderGeminiAPIKey
			}
		case config.ProviderOAuth:
			if req.ProviderID == "" && (existing.ProviderID == "" || existing.ProviderID == llm.ProviderGeminiAPIKey) {
				existing.ProviderID = llm.ProviderGoogleGeminiCLI
			}
		}
	}
	if req.ProviderID != "" {
		existing.ProviderID = llm.NormalizeGoogleProviderID(req.ProviderID)
	}
	if req.ModelID != "" {
		existing.ModelID = strings.TrimSpace(req.ModelID)
	}
	if req.EndpointPolicy != "" {
		existing.EndpointPolicy = llm.ResolveEndpointPolicy(req.EndpointPolicy)
	}
	if req.BraveAPIKey != "" && !isMasked(req.BraveAPIKey) {
		existing.BraveAPIKey = req.BraveAPIKey
	}
	if req.WebSearchEnabled != nil {
		existing.WebSearchEnabled = *req.WebSearchEnabled
	}
	if req.DiscordUseReply != nil {
		existing.DiscordUseReply = *req.DiscordUseReply
	}
	if req.DiscordStripLeadingBotMention != nil {
		existing.DiscordStripLeadingBotMention = *req.DiscordStripLeadingBotMention
	}
	if req.PerplexityAPIKey != "" && !isMasked(req.PerplexityAPIKey) {
		existing.PerplexityAPIKey = req.PerplexityAPIKey
	}
	existing.SearchProvider = req.SearchProvider
	if req.WebFetchEnabled != nil {
		existing.WebFetchEnabled = *req.WebFetchEnabled
	}
	if req.WebToolMode != "" {
		existing.WebToolMode = req.WebToolMode
	}
	existing.Timezone = req.Timezone
	existing.TimeFormat = req.TimeFormat
	existing.Location = req.Location
	if req.ContextFlushEnabled != nil {
		existing.ContextFlushEnabled = *req.ContextFlushEnabled
	}
	if req.ContextFlushThreshold != nil {
		existing.ContextFlushThreshold = *req.ContextFlushThreshold
	}
	if req.ContextWindowSize != nil {
		existing.ContextWindowSize = *req.ContextWindowSize
	}
	if req.PersonaLockEveryTurns != nil {
		existing.PersonaLockEveryTurns = *req.PersonaLockEveryTurns
	}
	if req.TripletOutputEnabled != nil {
		existing.TripletOutputEnabled = *req.TripletOutputEnabled
	}
	if req.NegativeConstraintEnabled != nil {
		existing.NegativeConstraintEnabled = *req.NegativeConstraintEnabled
	}
	if req.AnalysisVisibility != "" {
		existing.AnalysisVisibility = req.AnalysisVisibility
	}
	if req.NegativeConstraintDefault != "" {
		existing.NegativeConstraintDefault = req.NegativeConstraintDefault
	}
	if req.DiscordObserveEnabled != nil {
		existing.DiscordObserveEnabled = *req.DiscordObserveEnabled
	}
	if req.DiscordMemberIntentEnabled != nil {
		existing.DiscordMemberIntentEnabled = *req.DiscordMemberIntentEnabled
	}
	if req.DiscordPresenceIntentEnabled != nil {
		existing.DiscordPresenceIntentEnabled = *req.DiscordPresenceIntentEnabled
	}
	if req.DiscordObserveMaxResults != nil {
		existing.DiscordObserveMaxResults = *req.DiscordObserveMaxResults
	}
	if req.DiscordObserveMessageScope != "" {
		existing.DiscordObserveMessageScope = req.DiscordObserveMessageScope
	}
	if req.ToolGuardEnabled != nil {
		existing.ToolGuardEnabled = *req.ToolGuardEnabled
	}
	if req.MaxToolRounds != nil {
		existing.MaxToolRounds = *req.MaxToolRounds
	}
	if req.MaxWebSearchCallsPerTurn != nil {
		existing.MaxWebSearchCallsPerTurn = *req.MaxWebSearchCallsPerTurn
	}
	if req.ForceFinalAnswerAfterToolLimit != nil {
		existing.ForceFinalAnswerAfterToolLimit = *req.ForceFinalAnswerAfterToolLimit
	}
	if req.TransientRecoveryWaitEnabled != nil {
		existing.TransientRecoveryWaitEnabled = *req.TransientRecoveryWaitEnabled
	}
	if req.TransientRecoveryWaitMaxSeconds != nil {
		existing.TransientRecoveryWaitMaxSeconds = *req.TransientRecoveryWaitMaxSeconds
	}
	if req.DiscordShowInternalToolErrors != nil {
		existing.DiscordShowInternalToolErrors = *req.DiscordShowInternalToolErrors
	}
	if req.ScheduledMonitorMaxPerUser != nil {
		existing.ScheduledMonitorMaxPerUser = *req.ScheduledMonitorMaxPerUser
	}
	if req.FallbackEnabled != nil {
		existing.FallbackEnabled = *req.FallbackEnabled
		existing.FallbackModelEnabled = *req.FallbackEnabled
	}
	if req.FallbackModelEnabled != nil {
		existing.FallbackModelEnabled = *req.FallbackModelEnabled
		existing.FallbackEnabled = *req.FallbackModelEnabled
	}
	if req.FallbackModelID != "" {
		existing.FallbackModelID = strings.TrimSpace(req.FallbackModelID)
	}
	existing.FallbackModel = llm.NormalizeGoogleModelAlias(req.FallbackModel)
	if req.MCP != nil {
		prevMCP := existing.MCP
		existing.MCP = convertMCPSettingsUpdate(*req.MCP)
		resolveMaskedMCPSettingsSecrets(&existing.MCP, prevMCP)
	}
	if req.MemoryExtractionEnabled != nil {
		existing.MemoryExtractionEnabled = *req.MemoryExtractionEnabled
	}
	existing.MemoryExtractionModel = req.MemoryExtractionModel
	if req.MemoryRecentRounds != nil {
		existing.MemoryRecentRounds = *req.MemoryRecentRounds
	}
	existing.LogChannelID = req.LogChannelID
	if req.DiscordPersonaID != "" {
		if _, ok := h.personaService.Get(req.DiscordPersonaID); !ok {
			return fmt.Errorf("invalid discord persona id: %s", req.DiscordPersonaID)
		}
		existing.DiscordPersonaID = req.DiscordPersonaID
	}

	existing.ProviderType = resolveProviderTypeWithLegacyFallback(existing.ProviderType, existing.ProviderID, existing.GeminiAPIKey)
	existing.ProviderID, existing.GeminiModel, existing.ModelID = llm.ResolveProviderAndModel(
		existing.ProviderID,
		existing.ModelID,
		existing.GeminiModel,
	)
	existing.ProviderType = normalizeProviderTypeFromProviderID(existing.ProviderType, existing.ProviderID)

	fallbackProviderID, fallbackModel := llm.SplitProviderModelID(existing.FallbackModelID)
	if fallbackModel == "" {
		fallbackModel = llm.NormalizeGoogleModelAlias(existing.FallbackModel)
	}
	if fallbackProviderID == "" {
		fallbackProviderID = existing.ProviderID
	}
	if fallbackModel != "" {
		existing.FallbackModel = fallbackModel
		existing.FallbackModelID = llm.BuildProviderModelID(fallbackProviderID, fallbackModel)
	} else {
		existing.FallbackModel = ""
		existing.FallbackModelID = ""
	}
	existing.FallbackModelEnabled = existing.FallbackModelEnabled || existing.FallbackEnabled
	existing.FallbackEnabled = existing.FallbackModelEnabled

	if existing.EndpointPolicy == "" {
		existing.EndpointPolicy = llm.EndpointPolicyPrimaryThenSandbox
	}
	return nil
}

func (h *Handlers) validateUpdatedSettings(settings config.Settings) error {
	if err := validatePromptGuardSettings(
		settings.PersonaLockEveryTurns,
		settings.AnalysisVisibility,
		settings.NegativeConstraintEnabled,
		settings.NegativeConstraintDefault,
	); err != nil {
		return err
	}
	if err := validateDiscordObserveSettings(settings.DiscordObserveMaxResults, settings.DiscordObserveMessageScope); err != nil {
		return err
	}
	if err := validateMemoryRetentionSettings(settings.MemoryRecentRounds); err != nil {
		return err
	}
	if err := validateSchedulerSettings(settings.ScheduledMonitorMaxPerUser); err != nil {
		return err
	}
	if err := validateMCPSettings(settings.MCP); err != nil {
		return err
	}
	if err := validateWebToolMode(settings.WebToolMode); err != nil {
		return err
	}
	return validateToolLoopSettings(
		settings.MaxToolRounds,
		settings.MaxWebSearchCallsPerTurn,
		settings.TransientRecoveryWaitMaxSeconds,
	)
}

func (h *Handlers) saveAndHotSwapSettings(existing config.Settings) (map[string]string, error) {
	if h.settingsRuntimeApplier == nil {
		h.settingsRuntimeApplier = NewSettingsRuntimeApplier(h)
	}
	return h.settingsRuntimeApplier.SaveAndApply(existing)
}

func convertMCPSettingsUpdate(req MCPSettingsUpdateRequest) config.MCPSettings {
	servers := make([]config.MCPServerSetting, 0, len(req.Servers))
	for _, server := range req.Servers {
		servers = append(servers, config.MCPServerSetting{
			ID:            server.ID,
			Name:          server.Name,
			Enabled:       server.Enabled,
			Transport:     server.Transport,
			Command:       server.Command,
			Args:          append([]string(nil), server.Args...),
			Env:           cloneStringMap(server.Env),
			URL:           server.URL,
			Headers:       cloneStringMap(server.Headers),
			ToolAllowlist: append([]string(nil), server.ToolAllowlist...),
		})
	}
	return config.MCPSettings{
		Enabled:                 req.Enabled,
		VerboseLogging:          req.VerboseLogging,
		DiscoveryTimeoutSeconds: req.DiscoveryTimeoutSeconds,
		ToolCallTimeoutSeconds:  req.ToolCallTimeoutSeconds,
		Servers:                 servers,
	}
}

func resolveMaskedMCPSettingsSecrets(incoming *config.MCPSettings, existing config.MCPSettings) {
	if incoming == nil {
		return
	}
	for idx := range incoming.Servers {
		resolveMaskedMCPServerSecrets(&incoming.Servers[idx], existing)
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func resolveProviderTypeWithLegacyFallback(providerType, providerID, apiKey string) string {
	normalized := normalizeProviderTypeForSelection(providerType)
	if normalized != "" {
		return normalized
	}
	switch llm.NormalizeGoogleProviderID(providerID) {
	case llm.ProviderGeminiAPIKey:
		return config.ProviderAPIKey
	case llm.ProviderGoogleGeminiCLI, llm.ProviderGoogleAntigravity:
		return config.ProviderOAuth
	default:
		if strings.TrimSpace(apiKey) != "" {
			return config.ProviderAPIKey
		}
		return config.ProviderOAuth
	}
}

func normalizeProviderTypeFromProviderID(providerType, providerID string) string {
	switch llm.NormalizeGoogleProviderID(providerID) {
	case llm.ProviderGeminiAPIKey:
		return config.ProviderAPIKey
	case llm.ProviderGoogleGeminiCLI, llm.ProviderGoogleAntigravity:
		return config.ProviderOAuth
	default:
		return providerType
	}
}
