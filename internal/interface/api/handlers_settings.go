package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const oauthProbeTimeout = 8 * time.Second

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	searchProvider := settings.SearchProvider
	if searchProvider == "" {
		searchProvider = "auto"
	}
	discordPersonaID := settings.DiscordPersonaID
	if discordPersonaID == "" && h.discordPersona != nil {
		discordPersonaID = h.discordPersona.PersonaID()
	}

	writeJSON(w, http.StatusOK, SettingsResponse{
		GeminiAPIKey:                    maskKey(settings.GeminiAPIKey),
		GeminiModel:                     settings.GeminiModel,
		DiscordToken:                    maskKey(settings.DiscordToken),
		ProviderType:                    settings.ProviderType,
		BraveAPIKey:                     maskKey(settings.BraveAPIKey),
		WebSearchEnabled:                settings.WebSearchEnabled,
		DiscordUseReply:                 settings.DiscordUseReply,
		DiscordStripLeadingBotMention:   settings.DiscordStripLeadingBotMention,
		PerplexityAPIKey:                maskKey(settings.PerplexityAPIKey),
		SearchProvider:                  searchProvider,
		WebFetchEnabled:                 settings.WebFetchEnabled,
		WebToolMode:                     settings.WebToolMode,
		Timezone:                        settings.Timezone,
		TimeFormat:                      settings.TimeFormat,
		Location:                        settings.Location,
		ContextFlushEnabled:             settings.ContextFlushEnabled,
		ContextFlushThreshold:           settings.ContextFlushThreshold,
		ContextWindowSize:               settings.ContextWindowSize,
		LogChannelID:                    settings.LogChannelID,
		DiscordPersonaID:                discordPersonaID,
		MemoryExtractionEnabled:         settings.MemoryExtractionEnabled,
		MemoryExtractionModel:           settings.MemoryExtractionModel,
		MemoryRecentRounds:              settings.MemoryRecentRounds,
		PersonaLockEveryTurns:           settings.PersonaLockEveryTurns,
		TripletOutputEnabled:            settings.TripletOutputEnabled,
		AnalysisVisibility:              settings.AnalysisVisibility,
		NegativeConstraintEnabled:       settings.NegativeConstraintEnabled,
		NegativeConstraintDefault:       settings.NegativeConstraintDefault,
		DiscordObserveEnabled:           settings.DiscordObserveEnabled,
		DiscordMemberIntentEnabled:      settings.DiscordMemberIntentEnabled,
		DiscordPresenceIntentEnabled:    settings.DiscordPresenceIntentEnabled,
		DiscordObserveMaxResults:        settings.DiscordObserveMaxResults,
		DiscordObserveMessageScope:      settings.DiscordObserveMessageScope,
		ToolGuardEnabled:                settings.ToolGuardEnabled,
		MaxToolRounds:                   settings.MaxToolRounds,
		MaxWebSearchCallsPerTurn:        settings.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit:  settings.ForceFinalAnswerAfterToolLimit,
		TransientRecoveryWaitEnabled:    settings.TransientRecoveryWaitEnabled,
		TransientRecoveryWaitMaxSeconds: settings.TransientRecoveryWaitMaxSeconds,
		DiscordShowInternalToolErrors:   settings.DiscordShowInternalToolErrors,
		ScheduledMonitorMaxPerUser:      settings.ScheduledMonitorMaxPerUser,
		FallbackModelEnabled:            settings.FallbackModelEnabled,
		FallbackModel:                   settings.FallbackModel,
		MCP:                             toMCPSettingsResponse(settings.MCP),
	})
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	req, err := decodeSettingsUpdateRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	if err := h.applySettingsPatch(&existing, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateUpdatedSettings(existing); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.saveAndHotSwapSettings(existing)
	if err != nil {
		if isSettingsClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// activateProvider creates and hot-swaps the LLM provider based on settings.
func (h *Handlers) activateProvider(s config.Settings) error {
	provider, err := h.buildProviderForSettings(s, false)
	if err != nil {
		return err
	}
	h.providerManager.Update(provider)
	logAPI.Logf("provider hot-swapped: %s (model: %s)", provider.ModelInfo().ProviderName, provider.ModelInfo().Model)
	return nil
}

func (h *Handlers) buildProviderForSettings(s config.Settings, strictOAuthProbe bool) (llm.Provider, error) {
	model := s.GeminiModel
	if model == "" {
		model = "gemini-2.5-pro"
	}
	model = llm.NormalizeModelID(model)
	if model == "" {
		model = "gemini-2.5-pro"
	}

	providerType := normalizeProviderTypeForSelection(s.ProviderType)
	logAPI.Logf("build-provider: type=%s model=%s strict_oauth_probe=%v", providerType, model, strictOAuthProbe)

	switch providerType {
	case config.ProviderOAuth:
		return h.buildOAuthPoolProvider(s, model, strictOAuthProbe)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return fmt.Errorf("invalid API key: %w", err)
		}
		return provider, nil

	default:
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}
			return provider, nil
		}
		// Auto mode without API key falls back to OAuth credentials.
		return h.buildOAuthPoolProvider(s, model, strictOAuthProbe)
	}
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:   h.providerManager.IsConfigured(),
		ProviderName: info.ProviderName,
		Model:        info.Model,
	})
}

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	var oauthToken *llm.OAuthTokenData
	if data, err := h.oauthFlow.GetStatus(); err == nil {
		oauthToken = data
	}

	models, err := llm.ListGeminiModelsForProvider(r.Context(), settings.GeminiAPIKey, oauthToken, settings.ProviderType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models)
}

func (h *Handlers) buildOAuthPoolProvider(s config.Settings, model string, strictOAuthProbe bool) (llm.Provider, error) {
	logAPI.Logf("build-provider: switching to OAuth")
	poolPath := h.oauthFlow.PoolPath()
	tokens, err := llm.LoadOAuthPool(poolPath)
	if err != nil || len(tokens) == 0 {
		oauthData, statusErr := h.oauthFlow.GetStatus()
		if statusErr != nil || oauthData == nil {
			return nil, fmt.Errorf("no OAuth credentials available")
		}
		tokens = []*llm.OAuthTokenData{oauthData}
	}

	pool := app.NewProviderPool()
	pool.SetPoolStorePath(poolPath)
	if s.AutoRetryCount > 0 {
		pool.SetAutoRetryCount(s.AutoRetryCount)
	}
	pool.SetTransientRecoveryWait(
		s.TransientRecoveryWaitEnabled,
		time.Duration(s.TransientRecoveryWaitMaxSeconds)*time.Second,
	)
	if s.FallbackModelEnabled && s.FallbackModel != "" {
		pool.SetFallbackModel(llm.NormalizeModelID(s.FallbackModel))
	}

	probeSuccess := false
	probeNotFound := false
	for _, tokenData := range tokens {
		if err := llm.RefreshIfNeeded(h.ctx, tokenData, ""); err != nil {
			logAPI.Warnf("build-provider: token refresh failed for %s: %v", tokenData.Email, err)
			continue
		}
		if poolPath != "" && tokenData.Email != "" {
			if err := llm.UpdateOAuthPoolToken(poolPath, tokenData.Email, tokenData); err != nil {
				logAPI.Warnf("build-provider: persist refreshed token failed for %s: %v", tokenData.Email, err)
			}
		}
		p, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
		if err != nil {
			logAPI.Warnf("build-provider: provider init failed for %s: %v", tokenData.Email, err)
			continue
		}

		if strictOAuthProbe {
			if probeErr := h.probeOAuthModel(richContext(h.ctx), p); probeErr != nil {
				if isOAuthModelNotFoundError(probeErr) {
					probeNotFound = true
					logAPI.Warnf("build-provider: OAuth model probe 404 for %s: %v", tokenData.Email, probeErr)
					continue
				}
				return nil, fmt.Errorf("OAuth model probe failed for %s: %w", tokenData.Email, probeErr)
			}
			probeSuccess = true
		}

		pool.AddOrReplaceWithUsage(p, tokenData.Email, tokenData.UsageStats)
	}

	if pool.Size() == 0 {
		if strictOAuthProbe && probeNotFound {
			return nil, fmt.Errorf("OAuth model %q is not available (404 NOT_FOUND)", model)
		}
		return nil, fmt.Errorf("no OAuth accounts available")
	}
	if strictOAuthProbe && !probeSuccess {
		return nil, fmt.Errorf("OAuth model probe failed for all accounts")
	}
	return pool, nil
}

func (h *Handlers) probeOAuthModel(ctx context.Context, provider llm.Provider) error {
	probeCtx, cancel := context.WithTimeout(ctx, oauthProbeTimeout)
	defer cancel()
	_, err := provider.Chat(probeCtx, llm.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "ping"},
		},
		ToolMode: llm.ToolModeDisabled,
	})
	return err
}

func isOAuthModelNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "http 404") ||
		strings.Contains(lower, "\"status\": \"not_found\"") ||
		strings.Contains(lower, "\"status\":\"not_found\"") ||
		strings.Contains(lower, "requested entity was not found")
}

func normalizeProviderTypeForSelection(providerType string) string {
	switch strings.TrimSpace(strings.ToLower(providerType)) {
	case config.ProviderAPIKey:
		return config.ProviderAPIKey
	case config.ProviderOAuth, "cli":
		return config.ProviderOAuth
	default:
		return ""
	}
}

func richContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
