package provider

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// Add appends or replaces a provider in the pool.
func (pp *ProviderPool) Add(provider llm.Provider, email string) {
	pp.AddOrReplaceWithUsage(provider, email, nil)
}

// AddOrReplace inserts a new account or replaces an existing one with the same email.
func (pp *ProviderPool) AddOrReplace(provider llm.Provider, email string) {
	pp.AddOrReplaceWithUsage(provider, email, nil)
}

// AddOrReplaceWithUsage inserts/replaces a provider and optional persisted usage stats.
func (pp *ProviderPool) AddOrReplaceWithUsage(provider llm.Provider, email string, usage *llm.OAuthUsageStats) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	if len(pp.tools) > 0 {
		provider.RegisterTools(pp.tools)
	}

	for i := range pp.entries {
		if pp.entries[i].email != email {
			continue
		}
		pp.entries[i].provider = provider
		pp.entries[i].cooldownUntil = time.Time{}
		pp.entries[i].disabledUntil = time.Time{}
		pp.entries[i].disabledReason = ""
		pp.entries[i].consecutiveFailures = 0
		pp.entries[i].lastError = ""
		if usage != nil {
			pp.entries[i].usage = cloneUsageStats(usage)
		} else if pp.entries[i].usage == nil {
			pp.entries[i].usage = &llm.OAuthUsageStats{}
		}
		logPool.Logf("pool: replaced %s (%d account(s))", email, len(pp.entries))
		return
	}

	pp.entries = append(pp.entries, poolEntry{
		provider: provider,
		email:    email,
		usage:    cloneUsageStats(usage),
	})
	if pp.entries[len(pp.entries)-1].usage == nil {
		pp.entries[len(pp.entries)-1].usage = &llm.OAuthUsageStats{}
	}
	logPool.Logf("pool: added %s (%d account(s))", email, len(pp.entries))
}

// Remove removes a provider by email.
func (pp *ProviderPool) Remove(email string) bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	for i, e := range pp.entries {
		if e.email == email {
			pp.entries = append(pp.entries[:i], pp.entries[i+1:]...)
			if pp.current >= len(pp.entries) && len(pp.entries) > 0 {
				pp.current = 0
			}
			logPool.Logf("pool: removed %s (%d account(s))", email, len(pp.entries))
			return true
		}
	}
	return false
}

// Size returns the number of providers in the pool.
func (pp *ProviderPool) Size() int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return len(pp.entries)
}

// Entries returns the current status of all pool entries.
func (pp *ProviderPool) Entries() []PoolStatus {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	now := time.Now()
	result := make([]PoolStatus, len(pp.entries))
	for i, e := range pp.entries {
		status := "available"
		var cooldownUntil, disabledUntil, exhaustedUntil *time.Time

		isDisabled := !e.disabledUntil.IsZero() && e.disabledUntil.After(now)
		isCooldown := !e.cooldownUntil.IsZero() && e.cooldownUntil.After(now)

		if isDisabled {
			status = "disabled"
			t := e.disabledUntil
			disabledUntil = &t
		} else if isCooldown {
			status = "cooldown"
			t := e.cooldownUntil
			cooldownUntil = &t
		}

		// Backward compat: ExhaustedUntil = max(cooldownUntil, disabledUntil)
		effective := e.effectiveExhaustedUntil()
		if effective.After(now) {
			exhaustedUntil = &effective
		}

		var lastSuccessAt *time.Time
		if !e.lastSuccessAt.IsZero() {
			t := e.lastSuccessAt
			lastSuccessAt = &t
		}
		attempts24h, successes24h, failures24h, windowStart := summarizeUsage24h(e.usage, now)
		lastAttemptAt := usageLastAttemptAt(e.usage)
		attemptsTotal, successesTotal, failuresTotal := summarizeUsageTotals(e.usage)
		if windowStart.IsZero() && lastAttemptAt != nil {
			windowStart = lastAttemptAt.Add(-24 * time.Hour).Truncate(time.Hour)
		}
		result[i] = PoolStatus{
			Email:               e.email,
			Status:              status,
			ExhaustedUntil:      exhaustedUntil,
			CooldownUntil:       cooldownUntil,
			DisabledUntil:       disabledUntil,
			DisabledReason:      e.disabledReason,
			Current:             e.email == pp.lastUsedEmail,
			RequestCount:        attempts24h,
			TotalRequests:       attemptsTotal,
			LastResetTime:       windowStart,
			Attempts24h:         attempts24h,
			Successes24h:        successes24h,
			Failures24h:         failures24h,
			AttemptsTotal:       attemptsTotal,
			SuccessesTotal:      successesTotal,
			FailuresTotal:       failuresTotal,
			LastAttemptAt:       lastAttemptAt,
			ConsecutiveFailures: e.consecutiveFailures,
			LastError:           e.lastError,
			LastSuccessAt:       lastSuccessAt,
		}
	}
	return result
}

// SelectNextProvider chooses the next best available account.
func (pp *ProviderPool) SelectNextProvider() (llm.Provider, string, bool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	idx, ok := pp.selectNextProviderLocked(time.Now())
	if !ok {
		return nil, "", false
	}
	entry := pp.entries[idx]
	pp.current = (idx + 1) % len(pp.entries)
	pp.lastUsedEmail = entry.email
	return entry.provider, entry.email, true
}

// MarkTransientFailure marks a short-lived transient failure on an account.
func (pp *ProviderPool) MarkTransientFailure(email string, err error, cooldown time.Duration) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	entry := pp.findEntryByEmailLocked(email)
	if entry == nil {
		return
	}
	now := time.Now()
	entry.consecutiveFailures++
	entry.lastFailureAt = now
	entry.lastError = truncateError(err, 240)
	if cooldown > 0 {
		next := now.Add(cooldown)
		if entry.cooldownUntil.Before(next) {
			entry.cooldownUntil = next
		}
	}
}

// Chat implements llm.Provider with automatic failover.
func (pp *ProviderPool) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return tryPool(ctx, pp, func(p llm.Provider) (*llm.ChatResponse, error) {
		return p.Chat(ctx, req)
	})
}

// StreamChat implements llm.Provider with automatic failover.
func (pp *ProviderPool) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return tryPool(ctx, pp, func(p llm.Provider) (<-chan llm.StreamChunk, error) {
		return p.StreamChat(ctx, req)
	})
}

// StreamChatMultimodal implements llm.Provider with automatic failover.
func (pp *ProviderPool) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	return tryPool(ctx, pp, func(p llm.Provider) (<-chan llm.StreamChunk, error) {
		return p.StreamChatMultimodal(ctx, systemPrompt, messages, generation)
	})
}

// ModelInfo returns capabilities from the first available provider.
func (pp *ProviderPool) ModelInfo() llm.ModelCapabilities {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	if len(pp.entries) == 0 {
		return llm.ModelCapabilities{ProviderName: "pool (empty)"}
	}
	caps := pp.entries[0].provider.ModelInfo()
	caps.ProviderName = fmt.Sprintf("pool (%d accounts) — %s", len(pp.entries), caps.ProviderName)
	return caps
}

// EffectiveModel returns the currently effective model (primary or fallback).
func (pp *ProviderPool) EffectiveModel() string {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	if strings.TrimSpace(pp.effectiveModel) != "" {
		return pp.effectiveModel
	}
	if len(pp.entries) == 0 {
		return ""
	}
	return pp.entries[0].provider.ModelInfo().Model
}

// EffectiveEndpoint returns the endpoint used by the latest successful request.
func (pp *ProviderPool) EffectiveEndpoint() string {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.effectiveEndpoint
}

// LastFallbackReason returns the latest model fallback reason.
func (pp *ProviderPool) LastFallbackReason() string {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.lastFallbackReason
}

// RegisterTools registers tools with all providers in the pool.
func (pp *ProviderPool) RegisterTools(tools []domain.Tool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pp.tools = tools
	for _, e := range pp.entries {
		e.provider.RegisterTools(tools)
	}
}

// tryPool is a generic failover function.
func tryPool[T any](ctx context.Context, pp *ProviderPool, fn func(llm.Provider) (T, error)) (T, error) {
	var zero T

	// Circuit breaker: immediately reject if open.
	if pp.circuitBreaker != nil && !pp.circuitBreaker.allowRequest() {
		return zero, ErrCircuitOpen
	}

	pp.mu.RLock()
	total := len(pp.entries)
	pp.mu.RUnlock()
	if total == 0 {
		return zero, ErrProviderNotConfigured
	}

	waitRetryUsed := false
	modelCapacityHit := false
	modelFallbackTriggered := false
	var lastErr error

	for tried := 0; tried < total; tried++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return zero, err
			}
		}

		selection := pp.selectOrWaitProvider(ctx, waitRetryUsed)
		waitRetryUsed = selection.waitRetryUsed
		if selection.err != nil {
			return zero, selection.err
		}
		if selection.retry {
			tried--
			continue
		}

		result, err, errType := executeWithRetry(pp, selection.provider, selection.email, fn)
		if err == nil {
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordSuccess()
			}
			pp.recordEffectiveState(selection.provider, selection.provider.ModelInfo().Model, "", false)
			if waitRetryUsed {
				atomic.AddUint64(&pp.metricTransientWaitRetrySucceeded, 1)
				logPool.Logf("metric pool.transient_wait_retry_succeeded=1")
			}
			return result, nil
		}
		if isContextTerminated(err) {
			if waitRetryUsed {
				atomic.AddUint64(&pp.metricTransientWaitRetryFailed, 1)
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err
		}

		switch errType {
		case errorQuota:
			pp.handleQuotaError(selection.email, err)
			continue
		case errorBilling:
			pp.handleBillingError(selection.email, err)
			continue
		case errorPermissionDenied:
			pp.handlePermissionDeniedError(selection.email, err)
			continue
		case errorOverloaded:
			pp.handleOverloadedError(selection.email, err)
			continue
		case errorEmptyStream:
			pp.handleTransientError(selection.email, err)
			continue
		case errorModelCapacity:
			pp.handleModelCapacityError(selection.email, err)
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordFailure()
			}
			modelCapacityHit = true
			modelFallbackTriggered = true
			lastErr = err
		case errorModelUnavailable:
			modelFallbackTriggered = true
			lastErr = err
			logPool.Warnf("model unavailable for %s: %s", selection.email, truncateError(err, 220))
		case errorTransient:
			pp.handleTransientError(selection.email, err)
			continue
		case errorValidation:
			logPool.Warnf("validation error with %s: %v", selection.email, err)
			if waitRetryUsed {
				atomic.AddUint64(&pp.metricTransientWaitRetryFailed, 1)
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err
		default:
			if waitRetryUsed {
				atomic.AddUint64(&pp.metricTransientWaitRetryFailed, 1)
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err
		}

		// Model capacity: all accounts share the same issue, stop trying.
		if modelCapacityHit || modelFallbackTriggered {
			break
		}
	}

	if modelFallbackTriggered {
		fallbackReason := ""
		if lastErr != nil {
			fallbackReason = lastErr.Error()
		}

		// Attempt fallback model before giving up.
		if result, err := tryFallbackModel(ctx, pp, fn, fallbackReason); err == nil {
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordSuccess()
			}
			return result, nil
		}

		// Queue the request if enabled; wait for cooldown to expire.
		if modelCapacityHit {
			if result, err := enqueueAndRetry(ctx, pp, fn); err == nil {
				if pp.circuitBreaker != nil {
					pp.circuitBreaker.recordSuccess()
				}
				return result, nil
			}
		}

		if lastErr != nil {
			return zero, lastErr
		}
	}

	return zero, pp.exhaustedError(waitRetryUsed)
}

// enqueueAndRetry queues the request if the request queue is enabled,
// waits for the cooldown to expire, then retries once.
func enqueueAndRetry[T any](ctx context.Context, pp *ProviderPool, fn func(llm.Provider) (T, error)) (T, error) {
	var zero T

	pp.mu.RLock()
	enabled := pp.queueEnabled
	queue := pp.queue
	pp.mu.RUnlock()

	if !enabled || queue == nil {
		return zero, fmt.Errorf("request queue not enabled")
	}

	logPool.Logf("queue: enqueuing request (current depth: %d/%d)", queue.depth(), queue.capacity)

	pos, err := queue.enqueue(ctx)
	if err != nil {
		logPool.Warnf("queue: enqueue failed (position %d): %v", pos, err)
		return zero, err
	}

	logPool.Logf("queue: request dequeued (was position %d), retrying", pos)

	// After being dequeued, attempt the call normally (one attempt).
	provider, email, ok := pp.SelectNextProvider()
	if !ok {
		return zero, fmt.Errorf("no provider available after queue wait")
	}

	result, callErr := fn(provider)
	pp.recordAttempt(email, time.Now(), callErr == nil)
	if callErr == nil {
		pp.markSuccess(email, time.Now())
		pp.recordEffectiveState(provider, provider.ModelInfo().Model, "", false)
		logPool.Logf("queue: retry succeeded via %s", email)
		return result, nil
	}
	logPool.Warnf("queue: retry failed via %s: %s", email, truncateError(callErr, 200))
	return zero, callErr
}

// tryFallbackModel attempts the request on a fallback model using the first
// available provider that supports ModelSwitcher.
func tryFallbackModel[T any](ctx context.Context, pp *ProviderPool, fn func(llm.Provider) (T, error), reason string) (T, error) {
	var zero T

	pp.mu.RLock()
	fallbackModel := pp.fallbackModel
	pp.mu.RUnlock()

	if fallbackModel == "" {
		return zero, fmt.Errorf("no fallback model configured")
	}

	// Pick any provider regardless of exhaustion status (all are exhausted
	// due to global cooldown, but we need one to clone with a different model).
	pp.mu.RLock()
	if len(pp.entries) == 0 {
		pp.mu.RUnlock()
		return zero, fmt.Errorf("no providers available for fallback")
	}
	provider := pp.entries[0].provider
	email := pp.entries[0].email
	pp.mu.RUnlock()

	switcher, ok := provider.(llm.ModelSwitcher)
	if !ok {
		return zero, fmt.Errorf("provider %s does not support model switching", email)
	}

	fallbackProvider := switcher.WithModel(fallbackModel)
	logPool.Logf("attempting fallback model %s via account %s", fallbackModel, email)

	result, err := fn(fallbackProvider)
	if err != nil {
		logPool.Warnf("fallback model %s failed: %s", fallbackModel, truncateError(err, 200))
		return zero, fmt.Errorf("fallback model %s: %w", fallbackModel, err)
	}

	result = annotateFallbackResult(result, fallbackModel, reason)
	pp.recordEffectiveState(fallbackProvider, fallbackModel, reason, true)
	atomic.AddUint64(&pp.metricModelFallback, 1)
	logPool.Logf("metric pool.model_fallback=1")
	logPool.Logf("fallback model %s succeeded", fallbackModel)
	return result, nil
}

func annotateFallbackResult[T any](result T, fallbackModel, reason string) T {
	note := formatFallbackNotice(fallbackModel, reason)
	if strings.TrimSpace(note) == "" {
		return result
	}

	switch typed := any(result).(type) {
	case *llm.ChatResponse:
		if typed == nil {
			return result
		}
		if strings.TrimSpace(typed.Content) == "" {
			typed.Content = note
		} else {
			typed.Content += "\n\n" + note
		}
		return any(typed).(T)
	case <-chan llm.StreamChunk:
		return any(appendFallbackNoteToStream(typed, note)).(T)
	default:
		return result
	}
}

func appendFallbackNoteToStream(input <-chan llm.StreamChunk, note string) <-chan llm.StreamChunk {
	if input == nil || strings.TrimSpace(note) == "" {
		return input
	}
	output := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(output)
		for chunk := range input {
			if chunk.Done && chunk.Error == nil && len(chunk.ToolCalls) == 0 {
				// Prepend \n\n so the notice starts on its own line after the LLM response.
				// IsAnnotation=true prevents this metadata from being stored in conversation history.
				output <- llm.StreamChunk{Content: "\n\n" + note, IsAnnotation: true}
			}
			output <- chunk
		}
	}()
	return output
}

func formatFallbackNotice(fallbackModel, reason string) string {
	model := strings.TrimSpace(fallbackModel)
	if model == "" {
		return ""
	}
	why := normalizeFallbackReason(reason)
	if why == "" {
		why = "primary model failed"
	}
	return fmt.Sprintf("```\n[Model fallback]\n switched to %s\n%s\n```", model, why)
}

func normalizeFallbackReason(reason string) string {
	why := strings.Join(strings.Fields(strings.TrimSpace(reason)), " ")
	if len(why) > 180 {
		why = why[:180] + "..."
	}
	return why
}

func (pp *ProviderPool) recordEffectiveState(provider llm.Provider, model, fallbackReason string, usedFallback bool) {
	if pp == nil || provider == nil {
		return
	}
	effectiveModel := strings.TrimSpace(model)
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(provider.ModelInfo().Model)
	}
	effectiveEndpoint := ""
	if endpointAware, ok := provider.(interface{ EffectiveEndpoint() string }); ok {
		effectiveEndpoint = strings.TrimSpace(endpointAware.EffectiveEndpoint())
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.effectiveModel = effectiveModel
	pp.effectiveEndpoint = effectiveEndpoint
	if usedFallback {
		reasonText := normalizeFallbackReason(fallbackReason)
		if reasonText == "" {
			reasonText = "primary model failed"
		}
		pp.lastFallbackReason = reasonText
		pp.lastFallbackAt = time.Now().UTC()
	}
}
