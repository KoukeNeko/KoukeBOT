package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
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
	if req.PerplexityAPIKey != "" && !isMasked(req.PerplexityAPIKey) {
		existing.PerplexityAPIKey = req.PerplexityAPIKey
	}
	existing.SearchProvider = req.SearchProvider
	if req.WebFetchEnabled != nil {
		existing.WebFetchEnabled = *req.WebFetchEnabled
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
	if req.MemoryExtractionEnabled != nil {
		existing.MemoryExtractionEnabled = *req.MemoryExtractionEnabled
	}
	existing.MemoryExtractionModel = req.MemoryExtractionModel
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
	return validateToolLoopSettings(
		settings.MaxToolRounds,
		settings.MaxWebSearchCallsPerTurn,
		settings.TransientRecoveryWaitMaxSeconds,
	)
}

func (h *Handlers) saveAndHotSwapSettings(existing config.Settings) (map[string]string, error) {
	logAPI.Logf("settings: saving model=%s provider=%s websearch=%v reply=%v",
		existing.GeminiModel, existing.ProviderType, existing.WebSearchEnabled, existing.DiscordUseReply)

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
	h.chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		MaxToolRounds:                  existing.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       existing.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: existing.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  existing.DiscordShowInternalToolErrors,
	})
	if pool := h.providerManager.GetPool(); pool != nil {
		pool.SetTransientRecoveryWait(
			existing.TransientRecoveryWaitEnabled,
			time.Duration(existing.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
	}

	response := map[string]string{"status": "saved"}
	if err := h.activateProvider(existing); err != nil {
		logAPI.Warnf("settings: provider activation failed (settings saved): %v", err)
		response["warning"] = err.Error()
	}

	logAPI.Logf("settings: saved (warning=%v)", response["warning"] != "")
	return response, nil
}
