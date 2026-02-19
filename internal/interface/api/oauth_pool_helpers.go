package api

import (
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
		waitMax = 5 * time.Second
	}
	pool.SetTransientRecoveryWait(settings.TransientRecoveryWaitEnabled, waitMax)

	fallbackEnabled := settings.FallbackEnabled || settings.FallbackModelEnabled
	_, primaryModel, _ := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	fallbackModels := resolveFallbackModelChain(settings, providerID, primaryModel)
	if fallbackEnabled && len(fallbackModels) > 0 {
		pool.SetFallbackModels(fallbackModels)
	} else {
		pool.SetFallbackModels(nil)
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
	if _, mid := llm.SplitProviderModelID(s.ModelID); mid == "" {
		s.ModelID = llm.DefaultProviderModelID(s.ProviderID)
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
