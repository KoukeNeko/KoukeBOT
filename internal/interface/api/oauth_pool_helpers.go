package api

import (
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func (h *Handlers) loadSettingsOrDefault() config.Settings {
	if h.settingsStore == nil {
		return config.Settings{}
	}
	settings, err := h.settingsStore.Load()
	if err != nil {
		return config.Settings{}
	}
	return settings
}

func configureOAuthPoolFromSettings(pool *app.ProviderPool, settings config.Settings, providerID, poolPath string) {
	pool.SetPoolStorePath(poolPath)

	retries := settings.AutoRetryCount
	if retries <= 0 {
		retries = 3
	}
	pool.SetAutoRetryCount(retries)

	waitMax := time.Duration(settings.TransientRecoveryWaitMaxSeconds) * time.Second
	if waitMax <= 0 {
		waitMax = time.Duration(config.DefaultTransientRecoveryWaitMax) * time.Second
	}
	pool.SetTransientRecoveryWait(settings.TransientRecoveryWaitEnabled, waitMax)

	fallbackEnabled := settings.FallbackEnabled || settings.FallbackModelEnabled
	_, primaryModel, _ := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	fallbackModels := resolveFallbackModelIDChain(settings, providerID, primaryModel)
	if fallbackEnabled && len(fallbackModels) > 0 {
		pool.SetFallbackModels(fallbackModels)
		attachCrossProviderFallbacksFromPool(pool, poolPath, providerID, fallbackModels)
	} else {
		pool.SetFallbackModels(nil)
		pool.SetFallbackProviderMap(nil)
	}

	if settings.RequestQueueEnabled {
		capacity := settings.RequestQueueCapacity
		if capacity <= 0 {
			capacity = 10
		}
		timeout := time.Duration(settings.RequestQueueTimeoutS) * time.Second
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		pool.SetQueueConfig(true, capacity, timeout)
	} else {
		pool.SetQueueConfig(false, 0, 0)
	}

	if settings.CircuitBreakerEnabled {
		threshold := settings.CircuitBreakerFailureThreshold
		if threshold <= 0 {
			threshold = 5
		}
		openDuration := time.Duration(settings.CircuitBreakerOpenDurationS) * time.Second
		if openDuration <= 0 {
			openDuration = 60 * time.Second
		}
		pool.SetCircuitBreaker(threshold, openDuration)
	} else {
		pool.SetCircuitBreaker(0, 0)
	}
}

// attachCrossProviderFallbacksFromPool loads OAuth pool tokens and registers
// cross-provider fallback providers for fallback models that reference a
// different provider than the primary.
func attachCrossProviderFallbacksFromPool(
	pool *app.ProviderPool,
	poolPath string,
	primaryProviderID string,
	fallbackModels []string,
) {
	if pool == nil || len(fallbackModels) == 0 {
		return
	}

	primaryNormalized := llm.NormalizeGoogleProviderID(primaryProviderID)
	if primaryNormalized == "" {
		primaryNormalized = llm.ProviderGoogleGeminiCLI
	}

	// Collect unique cross-provider IDs from fallback models.
	needed := map[string]string{} // providerID -> first model name
	for _, modelTarget := range fallbackModels {
		pid, mid := llm.SplitProviderModelID(modelTarget)
		if pid == "" || mid == "" {
			continue
		}
		normalizedPID := llm.NormalizeGoogleProviderID(pid)
		if normalizedPID == primaryNormalized {
			continue
		}
		if _, exists := needed[normalizedPID]; !exists {
			needed[normalizedPID] = mid
		}
	}

	if len(needed) == 0 {
		return
	}

	// Load OAuth pool tokens to find cross-provider credentials.
	tokens, err := llm.LoadOAuthPool(poolPath)
	if err != nil || len(tokens) == 0 {
		return
	}

	for providerID, defaultModel := range needed {
		provider := buildCrossProviderFromPoolTokens(tokens, providerID, defaultModel)
		if provider != nil {
			pool.SetFallbackProvider(providerID, provider)
			logAPI.Logf("cross-provider fallback registered: %s (model: %s)", providerID, defaultModel)
		}
	}
}

// buildCrossProviderFromPoolTokens creates a provider for the given provider ID
// using token data from the OAuth pool (runtime path).
func buildCrossProviderFromPoolTokens(
	tokens []*llm.OAuthTokenData,
	providerID string,
	defaultModel string,
) llm.Provider {
	switch providerID {
	case llm.ProviderAnthropicSetupToken:
		for _, tokenData := range tokens {
			tokenPID := llm.NormalizeGoogleProviderID(tokenData.ProviderID)
			if tokenPID != llm.ProviderAnthropicSetupToken {
				continue
			}
			if err := llm.ValidateAnthropicSetupToken(tokenData.AccessToken); err != nil {
				continue
			}
			provider, err := llm.NewAnthropicSetupTokenProvider(tokenData.AccessToken, defaultModel)
			if err != nil {
				continue
			}
			return provider
		}

	case llm.ProviderAnthropicAPIKey:
		apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		if apiKey == "" {
			return nil
		}
		provider, err := llm.NewAnthropicAPIProvider(apiKey, defaultModel)
		if err != nil {
			return nil
		}
		return provider
	}

	return nil
}

func (h *Handlers) ensureOAuthPool(settings config.Settings, providerID, model string) *app.ProviderPool {
	pool := h.providerManager.GetPool()
	if pool == nil {
		pool = app.NewProviderPool()
		h.providerManager.Update(pool)
	}
	configureOAuthPoolFromSettings(pool, settings, providerID, h.oauthFlow.PoolPath())
	h.attachAIStudioOAuthFallback(pool, model)
	return pool
}

func (h *Handlers) persistOAuthProviderSelection(providerID string) {
	if h.settingsStore == nil {
		return
	}
	s, err := h.settingsStore.Load()
	if err != nil {
		return
	}
	s.ProviderType = config.ProviderOAuth
	s.ProviderID = llm.NormalizeGoogleProviderID(providerID)
	if s.ProviderID == "" {
		s.ProviderID = llm.ProviderGoogleAntigravity
	}

	modelProviderID, mid := llm.SplitProviderModelID(s.ModelID)
	if mid == "" || modelProviderID != s.ProviderID {
		s.ModelID = llm.DefaultProviderModelID(s.ProviderID)
		s.GeminiModel = ""
	}
	_, s.GeminiModel, s.ModelID = llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
	_ = h.settingsStore.Save(s)
}

func (h *Handlers) attachAIStudioOAuthFallback(pool *app.ProviderPool, model string) {
	if pool == nil {
		return
	}
	provider, err := llm.LoadAIStudioRPCProviderFromFile(h.aiStudioSessionPath, model)
	if err != nil {
		pool.SetOAuthExhaustionFallback(nil)
		logAPI.Warnf("AI Studio fallback disabled: %v", err)
		return
	}
	pool.SetOAuthExhaustionFallback(provider)
}
