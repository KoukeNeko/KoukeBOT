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

	providerID, model, modelID := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	fallbackModelID, fallbackModel, fallbackSecondModelID, fallbackSecondModel := resolveFallbackModelsForResponse(settings, providerID, model)

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
		GeminiModel:                     model,
		DiscordToken:                    maskKey(settings.DiscordToken),
		ProviderType:                    resolveProviderTypeForSettings(settings, providerID),
		ProviderID:                      providerID,
		ModelID:                         modelID,
		FallbackEnabled:                 settings.FallbackEnabled || settings.FallbackModelEnabled,
		FallbackModelID:                 fallbackModelID,
		EndpointPolicy:                  llm.ResolveEndpointPolicy(settings.EndpointPolicy),
		DeprecatedFields:                []string{"provider_type", "gemini_model", "fallback_model", "fallback_model_enabled"},
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
		FallbackModelEnabled:            settings.FallbackEnabled || settings.FallbackModelEnabled,
		FallbackModel:                   fallbackModel,
		FallbackSecondModelID:           fallbackSecondModelID,
		FallbackSecondModel:             fallbackSecondModel,
		RequestQueueEnabled:             settings.RequestQueueEnabled,
		RequestQueueCapacity:            settings.RequestQueueCapacity,
		RequestQueueTimeoutS:            settings.RequestQueueTimeoutS,
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
	providerID, model, modelID := llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
	normalizedProviderID := llm.NormalizeGoogleProviderID(providerID)
	providerType := resolveProviderTypeForSettings(s, providerID)
	logAPI.Logf(
		"build-provider: type=%s provider_id=%s model_id=%s strict_oauth_probe=%v",
		providerType,
		providerID,
		modelID,
		strictOAuthProbe,
	)

	if normalizedProviderID == llm.ProviderAIStudioRPC {
		provider, err := llm.LoadAIStudioRPCProviderFromFile(h.aiStudioSessionPath, model)
		if err != nil {
			return nil, fmt.Errorf("invalid AI Studio session: %w", err)
		}
		if provider == nil {
			return nil, fmt.Errorf("AI Studio session is not configured")
		}
		return provider, nil
	}

	// OpenClaw-aligned behavior:
	// - google-gemini-cli/* with explicit OAuth → Cloud Code Assist OAuth bridge.
	// - google-gemini-cli/* without OAuth → local Gemini CLI binary.
	// - google-antigravity/* → Cloud Code Assist OAuth bridge.
	if normalizedProviderID == llm.ProviderGoogleGeminiCLI &&
		normalizeProviderTypeForSelection(s.ProviderType) != config.ProviderOAuth {
		return h.buildGeminiCLIProvider(model)
	}

	switch providerType {
	case config.ProviderOAuth:
		return h.buildOAuthPoolProvider(s, providerID, model, strictOAuthProbe)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return nil, fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return nil, fmt.Errorf("invalid API key: %w", err)
		}
		return provider, nil

	default:
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return nil, fmt.Errorf("invalid API key: %w", err)
			}
			return provider, nil
		}
		// Auto mode without API key falls back to OAuth credentials.
		return h.buildOAuthPoolProvider(s, providerID, model, strictOAuthProbe)
	}
}

func (h *Handlers) buildGeminiCLIProvider(model string) (llm.Provider, error) {
	provider, err := llm.NewGeminiCLIProvider(model)
	if err != nil {
		return nil, fmt.Errorf("invalid Gemini CLI runtime: %w", err)
	}
	return provider, nil
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	settings := config.Settings{}
	if h.settingsStore != nil {
		if loaded, err := h.settingsStore.Load(); err == nil {
			settings = loaded
		}
	}
	activeProviderID, activeModel, activeModelID := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	effectiveModel := llm.NormalizeGoogleModelAlias(info.Model)
	if effectiveModel == "" {
		effectiveModel = activeModel
	}
	if observedModel, observedEndpoint, observedFallbackReason := h.resolveProviderObservability(); observedModel != "" {
		effectiveModel = observedModel
		effectiveEndpoint, lastFallbackReason := observedEndpoint, observedFallbackReason
		effectiveModelID := llm.BuildProviderModelID(activeProviderID, effectiveModel)
		writeJSON(w, http.StatusOK, ProviderStatusResponse{
			Configured:         h.providerManager.IsConfigured(),
			ProviderName:       info.ProviderName,
			Model:              effectiveModel,
			ActiveProviderID:   activeProviderID,
			ActiveModelID:      activeModelID,
			EffectiveModelID:   effectiveModelID,
			EffectiveEndpoint:  effectiveEndpoint,
			LastFallbackReason: lastFallbackReason,
		})
		return
	}
	effectiveModelID := llm.BuildProviderModelID(activeProviderID, effectiveModel)
	_, effectiveEndpoint, lastFallbackReason := h.resolveProviderObservability()

	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:         h.providerManager.IsConfigured(),
		ProviderName:       info.ProviderName,
		Model:              effectiveModel,
		ActiveProviderID:   activeProviderID,
		ActiveModelID:      activeModelID,
		EffectiveModelID:   effectiveModelID,
		EffectiveEndpoint:  effectiveEndpoint,
		LastFallbackReason: lastFallbackReason,
	})
}

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	providerID := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	if providerID == "" {
		providerID = settings.ProviderID
	}
	if providerID == "" {
		providerID = settings.ProviderType
	}
	providerID = llm.NormalizeGoogleProviderID(providerID)

	var oauthToken *llm.OAuthTokenData
	// Prefer provider-matched pool token (has ProjectID) for dynamic discovery.
	if poolPath := h.oauthFlow.PoolPath(); poolPath != "" {
		if tokens, err := llm.LoadOAuthPool(poolPath); err == nil && len(tokens) > 0 {
			oauthToken = selectOAuthTokenForProvider(tokens, providerID)
		}
	}
	if oauthToken == nil {
		if data, err := h.oauthFlow.GetStatus(); err == nil {
			oauthToken = data
		}
	}

	models, err := llm.ListGeminiModelsForProvider(r.Context(), settings.GeminiAPIKey, oauthToken, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models)
}

func (h *Handlers) buildOAuthPoolProvider(s config.Settings, providerID, model string, strictOAuthProbe bool) (llm.Provider, error) {
	logAPI.Logf("build-provider: switching to OAuth provider=%s", providerID)
	targetProviderID := llm.NormalizeGoogleProviderID(providerID)
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
	configureOAuthPoolFromSettings(pool, s, providerID, poolPath)
	h.attachAIStudioOAuthFallback(pool, model)
	fallbackEnabled := s.FallbackEnabled || s.FallbackModelEnabled
	fallbackModels := resolveFallbackModelChain(s, providerID, model)

	probeSuccess := false
	probeNotFound := false
	for _, tokenData := range tokens {
		tokenProviderID := llm.NormalizeGoogleProviderID(tokenData.ProviderID)
		if tokenProviderID != "" && tokenProviderID != targetProviderID {
			continue
		}

		if err := llm.RefreshIfNeeded(h.ctx, tokenData, ""); err != nil {
			logAPI.Warnf("build-provider: token refresh failed for %s: %v", tokenData.Email, err)
			continue
		}
		if poolPath != "" && tokenData.Email != "" {
			if err := llm.UpdateOAuthPoolToken(poolPath, tokenData.Email, tokenData); err != nil {
				logAPI.Warnf("build-provider: persist refreshed token failed for %s: %v", tokenData.Email, err)
			}
		}
		p, err := llm.NewGeminiOAuthProviderWithProvider(
			h.ctx,
			tokenData,
			providerID,
			model,
			s.EndpointPolicy,
		)
		if err != nil {
			logAPI.Warnf("build-provider: provider init failed for %s: %v", tokenData.Email, err)
			continue
		}

		if strictOAuthProbe {
			if probeErr := h.probeOAuthProviderStrict(richContext(h.ctx), p, model, fallbackModels, fallbackEnabled); probeErr != nil {
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
		return nil, fmt.Errorf("no OAuth accounts available for provider %s", targetProviderID)
	}
	if strictOAuthProbe && !probeSuccess {
		return nil, fmt.Errorf("OAuth model probe failed for all accounts")
	}
	return pool, nil
}

func selectOAuthTokenForProvider(tokens []*llm.OAuthTokenData, providerID string) *llm.OAuthTokenData {
	if len(tokens) == 0 {
		return nil
	}
	targetProviderID := llm.NormalizeGoogleProviderID(providerID)
	if targetProviderID == "" {
		return tokens[0]
	}
	for _, token := range tokens {
		if llm.NormalizeGoogleProviderID(token.ProviderID) == targetProviderID {
			return token
		}
	}
	for _, token := range tokens {
		if strings.TrimSpace(token.ProviderID) == "" {
			return token
		}
	}
	return tokens[0]
}

func (h *Handlers) probeOAuthProviderStrict(
	ctx context.Context,
	provider llm.Provider,
	primaryModel string,
	fallbackModels []string,
	fallbackEnabled bool,
) error {
	if err := h.probeOAuthTextModel(ctx, provider); err != nil {
		return h.handleOAuthProbeErrorWithFallback(ctx, provider, err, primaryModel, fallbackModels, fallbackEnabled)
	}
	if err := h.probeOAuthMultimodalModel(ctx, provider); err != nil {
		return h.handleOAuthProbeErrorWithFallback(ctx, provider, err, primaryModel, fallbackModels, fallbackEnabled)
	}
	return nil
}

func (h *Handlers) handleOAuthProbeErrorWithFallback(
	ctx context.Context,
	provider llm.Provider,
	probeErr error,
	primaryModel string,
	fallbackModels []string,
	fallbackEnabled bool,
) error {
	if !isOAuthModelNotFoundError(probeErr) {
		return probeErr
	}
	if !fallbackEnabled || len(fallbackModels) == 0 {
		return probeErr
	}
	switcher, ok := provider.(llm.ModelSwitcher)
	if !ok {
		return fmt.Errorf("fallback probe requested but provider does not support model switching: %w", probeErr)
	}

	primary := llm.NormalizeGoogleModelAlias(primaryModel)
	for _, candidate := range fallbackModels {
		fallbackModel := llm.NormalizeGoogleModelAlias(candidate)
		if fallbackModel == "" {
			continue
		}
		if primary != "" && fallbackModel == primary {
			continue
		}

		fallbackProvider := switcher.WithModel(fallbackModel)
		if err := h.probeOAuthTextModel(ctx, fallbackProvider); err != nil {
			if isOAuthModelNotFoundError(err) {
				continue
			}
			return fmt.Errorf("fallback text probe failed (%s): %w", fallbackModel, err)
		}
		if err := h.probeOAuthMultimodalModel(ctx, fallbackProvider); err != nil {
			if isOAuthModelNotFoundError(err) {
				continue
			}
			return fmt.Errorf("fallback multimodal probe failed (%s): %w", fallbackModel, err)
		}

		return nil
	}
	return probeErr
}

func (h *Handlers) probeOAuthModel(ctx context.Context, provider llm.Provider) error {
	return h.probeOAuthTextModel(ctx, provider)
}

func (h *Handlers) probeOAuthTextModel(ctx context.Context, provider llm.Provider) error {
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

func (h *Handlers) probeOAuthMultimodalModel(ctx context.Context, provider llm.Provider) error {
	probeCtx, cancel := context.WithTimeout(ctx, oauthProbeTimeout)
	defer cancel()

	ch, err := provider.StreamChatMultimodal(probeCtx, "", []domain.MultimodalMessage{
		{
			Role: domain.RoleUser,
			Parts: []domain.MessagePart{
				{Text: "ping"},
				{
					ImageData: &domain.ImageInput{
						Filename: "probe.png",
						MimeType: "image/png",
						Data:     minimalProbePNG(),
					},
				},
			},
		},
	}, domain.GenerationConfig{})
	if err != nil {
		return err
	}
	for chunk := range ch {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Done {
			return nil
		}
	}
	return nil
}

func minimalProbePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d,
		0xb1, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
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

func resolveProviderTypeForSettings(settings config.Settings, providerID string) string {
	switch llm.NormalizeGoogleProviderID(providerID) {
	case llm.ProviderGeminiAPIKey:
		return config.ProviderAPIKey
	case llm.ProviderGoogleGeminiCLI, llm.ProviderGoogleAntigravity, llm.ProviderAIStudioRPC:
		return config.ProviderOAuth
	}

	switch normalizeProviderTypeForSelection(settings.ProviderType) {
	case config.ProviderAPIKey:
		return config.ProviderAPIKey
	case config.ProviderOAuth:
		return config.ProviderOAuth
	default:
		if strings.TrimSpace(settings.GeminiAPIKey) != "" {
			return config.ProviderAPIKey
		}
		return config.ProviderOAuth
	}
}

func resolveFallbackModelsForResponse(settings config.Settings, providerID, primaryModel string) (string, string, string, string) {
	modelIDs := config.ResolveFallbackModelIDChain(settings, providerID, primaryModel)
	models := config.ResolveFallbackModelChain(settings, providerID, primaryModel)

	var firstModelID, firstModel, secondModelID, secondModel string
	if len(models) > 0 {
		firstModel = models[0]
		firstModelID = modelIDs[0]
	}
	if len(models) > 1 {
		secondModel = models[1]
		secondModelID = modelIDs[1]
	}
	return firstModelID, firstModel, secondModelID, secondModel
}

func resolveFallbackModelChain(settings config.Settings, providerID, primaryModel string) []string {
	return config.ResolveFallbackModelChain(settings, providerID, primaryModel)
}

func (h *Handlers) resolveProviderObservability() (model string, endpoint string, fallbackReason string) {
	provider := h.providerManager.Provider()
	if provider == nil {
		return "", "", ""
	}
	if modelAware, ok := provider.(interface{ EffectiveModel() string }); ok {
		model = llm.NormalizeGoogleModelAlias(modelAware.EffectiveModel())
	}
	if endpointAware, ok := provider.(interface{ EffectiveEndpoint() string }); ok {
		endpoint = endpointAware.EffectiveEndpoint()
	}
	if fallbackAware, ok := provider.(interface{ LastFallbackReason() string }); ok {
		fallbackReason = fallbackAware.LastFallbackReason()
	}
	return model, endpoint, fallbackReason
}
