package bootstrap

import (
	"context"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func initProvider(ctx context.Context, settings config.Settings, pm *app.ProviderManager, model, port string, paths Paths, logs Loggers) {
	switch settings.ProviderType {
	case config.ProviderOAuth:
		if initOAuthProvider(ctx, settings, pm, model, paths, logs) {
			return
		}
		logs.LLM.Warnf("OAuth selected but not available — configure at http://localhost:%s/settings", port)

	case config.ProviderAPIKey:
		if initAPIKeyProvider(ctx, settings.GeminiAPIKey, pm, model, logs) {
			return
		}
		logs.LLM.Warnf("API key selected but invalid — configure at http://localhost:%s/settings", port)

	default:
		if settings.GeminiAPIKey != "" && initAPIKeyProvider(ctx, settings.GeminiAPIKey, pm, model, logs) {
			return
		}
		if initOAuthProvider(ctx, settings, pm, model, paths, logs) {
			return
		}
		logs.LLM.Warnf("no LLM credentials — configure at http://localhost:%s/settings", port)
	}
}

func initAPIKeyProvider(ctx context.Context, apiKey string, pm *app.ProviderManager, model string, logs Loggers) bool {
	if apiKey == "" {
		return false
	}
	provider, err := llm.NewGeminiAPIProvider(ctx, apiKey, model)
	if err != nil {
		logs.LLM.Warnf("failed to init Gemini API provider: %v", err)
		return false
	}
	pm.Update(provider)
	logs.LLM.Logf("%s (model: %s)", provider.ModelInfo().ProviderName, model)
	return true
}

func initOAuthProvider(
	ctx context.Context,
	settings config.Settings,
	pm *app.ProviderManager,
	model string,
	paths Paths,
	logs Loggers,
) bool {
	llm.MigrateSingleTokenToPool(paths.OAuthTokenPath, paths.OAuthPoolPath)

	tokens, err := llm.LoadOAuthPool(paths.OAuthPoolPath)
	if err != nil {
		logs.LLM.Warnf("failed to load OAuth pool: %v", err)
	}

	if len(tokens) == 0 {
		oauthData, err := llm.LoadStoredToken(paths.OAuthTokenPath)
		if err != nil || oauthData == nil {
			return false
		}
		tokens = []*llm.OAuthTokenData{oauthData}
	}

	pool := app.NewProviderPool()
	pool.SetPoolStorePath(paths.OAuthPoolPath)

	if settings.AutoRetryCount > 0 {
		pool.SetAutoRetryCount(settings.AutoRetryCount)
	}
	pool.SetTransientRecoveryWait(
		settings.TransientRecoveryWaitEnabled,
		time.Duration(settings.TransientRecoveryWaitMaxSeconds)*time.Second,
	)
	if settings.FallbackModel != "" {
		pool.SetFallbackModel(settings.FallbackModel)
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
	}

	for _, tokenData := range tokens {
		if err := llm.RefreshIfNeeded(ctx, tokenData, ""); err != nil {
			logs.LLM.Warnf("OAuth token refresh failed for %s: %v", tokenData.Email, err)
			continue
		}
		if tokenData.Email != "" {
			if err := llm.UpdateOAuthPoolToken(paths.OAuthPoolPath, tokenData.Email, tokenData); err != nil {
				logs.LLM.Warnf("failed to persist refreshed pool token for %s: %v", tokenData.Email, err)
			}
		}
		provider, err := llm.NewGeminiOAuthProvider(ctx, tokenData, model)
		if err != nil {
			logs.LLM.Warnf("failed to init OAuth provider for %s: %v", tokenData.Email, err)
			continue
		}
		pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	}

	if pool.Size() == 0 {
		return false
	}

	pm.Update(pool)
	logs.LLM.Logf("OAuth pool: %d account(s) (model: %s)", pool.Size(), model)
	return true
}
