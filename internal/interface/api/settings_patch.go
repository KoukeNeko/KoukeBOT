package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
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
		existing.GeminiModel = req.GeminiModel
	}
	if req.DiscordToken != "" && !isMasked(req.DiscordToken) {
		existing.DiscordToken = req.DiscordToken
	}
	if req.ProviderType != "" {
		existing.ProviderType = req.ProviderType
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
