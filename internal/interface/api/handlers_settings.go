package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// activateProvider creates and hot-swaps the LLM provider based on settings.
func (h *Handlers) activateProvider(s config.Settings) error {
	model := s.GeminiModel
	if model == "" {
		model = "gemini-2.5-pro"
	}

	logAPI.Logf("activate-provider: type=%s model=%s", s.ProviderType, model)

	switch s.ProviderType {
	case config.ProviderOAuth:
		logAPI.Logf("activate-provider: switching to OAuth")
		poolPath := h.oauthFlow.PoolPath()
		tokens, err := llm.LoadOAuthPool(poolPath)
		if err != nil || len(tokens) == 0 {
			oauthData, err := h.oauthFlow.GetStatus()
			if err != nil || oauthData == nil {
				return fmt.Errorf("no OAuth credentials available")
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
		for _, tokenData := range tokens {
			if err := llm.RefreshIfNeeded(h.ctx, tokenData, ""); err != nil {
				logAPI.Warnf("activate-provider: token refresh failed for %s: %v", tokenData.Email, err)
				continue
			}
			if poolPath != "" && tokenData.Email != "" {
				if err := llm.UpdateOAuthPoolToken(poolPath, tokenData.Email, tokenData); err != nil {
					logAPI.Warnf("activate-provider: persist refreshed token failed for %s: %v", tokenData.Email, err)
				}
			}
			p, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
			if err != nil {
				logAPI.Warnf("activate-provider: provider init failed for %s: %v", tokenData.Email, err)
				continue
			}
			pool.AddOrReplaceWithUsage(p, tokenData.Email, tokenData.UsageStats)
		}
		if pool.Size() == 0 {
			return fmt.Errorf("no OAuth accounts available")
		}
		h.providerManager.Update(pool)
		logAPI.Logf("provider hot-swapped: OAuth pool (%d accounts, model: %s)", pool.Size(), model)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return fmt.Errorf("invalid API key: %w", err)
		}
		h.providerManager.Update(provider)
		logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)

	default:
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}
			h.providerManager.Update(provider)
			logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)
		}
	}
	return nil
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:   h.providerManager.IsConfigured(),
		ProviderName: info.ProviderName,
		Model:        info.ProviderName,
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

	models, err := llm.ListGeminiModels(r.Context(), settings.GeminiAPIKey, oauthToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models)
}
