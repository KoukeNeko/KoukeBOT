package provider

import (
	"context"
	"fmt"
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
		pp.entries[i].exhaustedUntil = time.Time{}
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
		var until *time.Time
		if !e.exhaustedUntil.IsZero() && e.exhaustedUntil.After(now) {
			status = "exhausted"
			t := e.exhaustedUntil
			until = &t
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
			ExhaustedUntil:      until,
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
		if entry.exhaustedUntil.Before(next) {
			entry.exhaustedUntil = next
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
		case errorModelCapacity:
			pp.handleModelCapacityError(selection.email, err)
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordFailure()
			}
			modelCapacityHit = true
			lastErr = err
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
		if modelCapacityHit {
			break
		}
	}

	if modelCapacityHit {
		// Attempt fallback model before giving up.
		if result, err := tryFallbackModel(ctx, pp, fn); err == nil {
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordSuccess()
			}
			return result, nil
		}

		// Queue the request if enabled; wait for cooldown to expire.
		if result, err := enqueueAndRetry(ctx, pp, fn); err == nil {
			if pp.circuitBreaker != nil {
				pp.circuitBreaker.recordSuccess()
			}
			return result, nil
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

	pos, err := queue.enqueue(ctx)
	if err != nil {
		return zero, err
	}

	logPool.Logf("request dequeued (was position %d), retrying", pos)

	// After being dequeued, attempt the call normally (one attempt).
	provider, email, ok := pp.SelectNextProvider()
	if !ok {
		return zero, fmt.Errorf("no provider available after queue wait")
	}

	result, callErr := fn(provider)
	pp.recordAttempt(email, time.Now(), callErr == nil)
	if callErr == nil {
		pp.markSuccess(email, time.Now())
		return result, nil
	}
	return zero, callErr
}

// tryFallbackModel attempts the request on a fallback model using the first
// available provider that supports ModelSwitcher.
func tryFallbackModel[T any](ctx context.Context, pp *ProviderPool, fn func(llm.Provider) (T, error)) (T, error) {
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

	logPool.Logf("fallback model %s succeeded", fallbackModel)
	return result, nil
}
