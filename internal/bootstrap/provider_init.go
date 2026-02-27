package bootstrap

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func initProvider(
	ctx context.Context,
	settings config.Settings,
	pm *app.ProviderManager,
	providerID string,
	model string,
	port string,
	paths Paths,
	logs Loggers,
) {
	if llm.NormalizeGoogleProviderID(providerID) == llm.ProviderAIStudioRPC {
		if initAIStudioProvider(pm, paths.AIStudioSessionPath, model, logs) {
			return
		}
		logs.LLM.Warnf("AI Studio RPC selected but session is unavailable — configure at http://localhost:%s/settings", port)
		return
	}

	if llm.NormalizeGoogleProviderID(providerID) == llm.ProviderGoogleGeminiCLI {
		// When provider_type is explicitly "oauth", route to OAuth pool
		// instead of local CLI binary.
		if !strings.EqualFold(strings.TrimSpace(settings.ProviderType), config.ProviderOAuth) {
			if initGeminiCLIProvider(pm, model, logs) {
				return
			}
			logs.LLM.Warnf("Google Gemini CLI selected but local gemini binary unavailable — configure at http://localhost:%s/settings", port)
			return
		}
	}

	switch resolveBootstrapProviderType(settings, providerID) {
	case config.ProviderOAuth:
		if initOAuthProvider(ctx, settings, pm, providerID, model, paths, logs) {
			return
		}
		logs.LLM.Warnf("OAuth selected but not available — configure at http://localhost:%s/settings", port)

	case config.ProviderAPIKey:
		if initAPIKeyProvider(ctx, settings, providerID, pm, model, logs) {
			return
		}
		logs.LLM.Warnf("API key selected but invalid — configure at http://localhost:%s/settings", port)

	default:
		if (settings.GeminiAPIKey != "" || strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "") &&
			initAPIKeyProvider(ctx, settings, providerID, pm, model, logs) {
			return
		}
		if initOAuthProvider(ctx, settings, pm, providerID, model, paths, logs) {
			return
		}
		logs.LLM.Warnf("no LLM credentials — configure at http://localhost:%s/settings", port)
	}
}

func initAIStudioProvider(pm *app.ProviderManager, sessionPath, model string, logs Loggers) bool {
	provider, err := llm.LoadAIStudioRPCProviderFromFile(sessionPath, model)
	if err != nil {
		logs.LLM.Warnf("failed to init AI Studio RPC provider: %v", err)
		return false
	}
	if provider == nil {
		return false
	}
	pm.Update(provider)
	logs.LLM.Logf("AI Studio RPC provider (model: %s)", provider.ModelInfo().Model)
	return true
}

func initGeminiCLIProvider(pm *app.ProviderManager, model string, logs Loggers) bool {
	provider, err := llm.NewGeminiCLIProvider(model)
	if err != nil {
		logs.LLM.Warnf("failed to init Gemini CLI provider: %v", err)
		return false
	}
	pm.Update(provider)
	logs.LLM.Logf("%s (model: %s)", provider.ModelInfo().ProviderName, provider.ModelInfo().Model)
	return true
}

func initAPIKeyProvider(
	ctx context.Context,
	settings config.Settings,
	providerID string,
	pm *app.ProviderManager,
	model string,
	logs Loggers,
) bool {
	if llm.NormalizeGoogleProviderID(providerID) == llm.ProviderOpenAIAPIKey {
		return initOpenAIAPIKeyProvider(strings.TrimSpace(os.Getenv("OPENAI_API_KEY")), pm, model, logs)
	}
	return initGeminiAPIKeyProvider(ctx, settings.GeminiAPIKey, pm, model, logs)
}

func initGeminiAPIKeyProvider(ctx context.Context, apiKey string, pm *app.ProviderManager, model string, logs Loggers) bool {
	if !hasGeminiAPIKeyConfigured(apiKey) {
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

func hasGeminiAPIKeyConfigured(apiKey string) bool {
	if strings.TrimSpace(apiKey) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("GOOGLE_API_KEY")) != ""
}

func initOpenAIAPIKeyProvider(apiKey string, pm *app.ProviderManager, model string, logs Loggers) bool {
	if apiKey == "" {
		return false
	}
	provider, err := llm.NewOpenAIAPIProvider(apiKey, model)
	if err != nil {
		logs.LLM.Warnf("failed to init OpenAI API provider: %v", err)
		return false
	}
	pm.Update(provider)
	logs.LLM.Logf("%s (model: %s)", provider.ModelInfo().ProviderName, provider.ModelInfo().Model)
	return true
}

func initOAuthProvider(
	ctx context.Context,
	settings config.Settings,
	pm *app.ProviderManager,
	providerID string,
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
	if fallbackModels := resolveBootstrapFallbackModels(settings, providerID, model); len(fallbackModels) > 0 && (settings.FallbackEnabled || settings.FallbackModelEnabled) {
		pool.SetFallbackModels(fallbackModels)
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
	attachAIStudioFallbackProvider(pool, paths.AIStudioSessionPath, model, logs)

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
		provider, err := llm.NewGeminiOAuthProviderWithProvider(
			ctx,
			tokenData,
			providerID,
			model,
			settings.EndpointPolicy,
		)
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

func attachAIStudioFallbackProvider(pool *app.ProviderPool, sessionPath, model string, logs Loggers) {
	if pool == nil {
		return
	}
	provider, err := llm.LoadAIStudioRPCProviderFromFile(sessionPath, model)
	if err != nil {
		pool.SetOAuthExhaustionFallback(nil)
		logs.LLM.Warnf("AI Studio fallback disabled: %v", err)
		return
	}
	pool.SetOAuthExhaustionFallback(provider)
	if provider != nil {
		logs.LLM.Logf("AI Studio OAuth exhaustion fallback enabled (model: %s)", provider.ModelInfo().Model)
	}
}

func resolveBootstrapProviderType(settings config.Settings, providerID string) string {
	switch llm.NormalizeGoogleProviderID(providerID) {
	case llm.ProviderOpenAIAPIKey:
		return config.ProviderAPIKey
	case llm.ProviderGeminiAPIKey:
		return config.ProviderAPIKey
	case llm.ProviderGoogleAntigravity:
		return config.ProviderOAuth
	case llm.ProviderGoogleGeminiCLI, llm.ProviderAIStudioRPC:
		return config.ProviderOAuth
	}
	switch settings.ProviderType {
	case config.ProviderAPIKey:
		return config.ProviderAPIKey
	case config.ProviderOAuth, "cli":
		return config.ProviderOAuth
	default:
		if settings.GeminiAPIKey != "" || strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
			return config.ProviderAPIKey
		}
		return config.ProviderOAuth
	}
}

func resolveBootstrapFallbackModels(settings config.Settings, providerID, primaryModel string) []string {
	return config.ResolveFallbackModelChain(settings, providerID, primaryModel)
}
