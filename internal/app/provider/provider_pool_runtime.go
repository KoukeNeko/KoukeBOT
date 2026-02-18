package provider

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type providerSelection struct {
	provider      llm.Provider
	email         string
	waitRetryUsed bool
	retry         bool
	err           error
}

func (pp *ProviderPool) selectOrWaitProvider(ctx context.Context, waitRetryUsed bool) providerSelection {
	provider, email, ok := pp.SelectNextProvider()
	if ok {
		return providerSelection{
			provider:      provider,
			email:         email,
			waitRetryUsed: waitRetryUsed,
		}
	}

	now := time.Now()
	pp.mu.RLock()
	waitDuration, canWait := pp.shortTransientWaitLocked(now)
	totalEntries := len(pp.entries)
	pp.mu.RUnlock()

	if canWait && !waitRetryUsed {
		atomic.AddUint64(&pp.metricTransientWaitRetry, 1)
		logPool.Logf("metric pool.transient_wait_retry=1")
		logPool.Warnf("all %d account(s) cooling down transiently — waiting %s before one retry", totalEntries, formatDuration(waitDuration))
		if err := waitForCooldown(ctx, waitDuration); err != nil {
			return providerSelection{err: err}
		}
		return providerSelection{
			waitRetryUsed: true,
			retry:         true,
		}
	}

	return providerSelection{
		waitRetryUsed: waitRetryUsed,
		err:           pp.exhaustedError(waitRetryUsed),
	}
}

func waitForCooldown(ctx context.Context, waitDuration time.Duration) error {
	timer := time.NewTimer(waitDuration)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	if ctx == nil {
		<-timer.C
		return nil
	}

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func executeWithRetry[T any](
	pp *ProviderPool,
	provider llm.Provider,
	email string,
	fn func(llm.Provider) (T, error),
) (T, error, errorType) {
	result, err := fn(provider)
	pp.recordAttempt(email, time.Now(), err == nil)

	if err == nil {
		pp.markSuccess(email, time.Now())
		return result, nil, errorNone
	}

	initialType := classifyError(err)
	if initialType != errorQuota {
		return result, err, initialType
	}

	resetDuration := parseResetDelay(err.Error())
	if resetDuration <= 30*time.Second {
		retryCount := pp.getAutoRetryCount()
		for retry := 0; retry < retryCount; retry++ {
			wait := exponentialBackoff(resetDuration, retry)
			logPool.Logf("account %s: short delay (%s), retry %d/%d...", email, formatDuration(wait), retry+1, retryCount)
			time.Sleep(wait)

			result, err = fn(provider)
			pp.recordAttempt(email, time.Now(), err == nil)
			if err == nil {
				pp.markSuccess(email, time.Now())
				return result, nil, errorNone
			}
			if classifyError(err) != errorQuota {
				break
			}
			resetDuration = parseResetDelay(err.Error())
			if resetDuration > 30*time.Second {
				break
			}
		}
	}

	return result, err, errorQuota
}

func (pp *ProviderPool) handleQuotaError(email string, err error) {
	// Use server-provided reset delay if available, otherwise escalate based on error count.
	serverDelay := parseResetDelay(err.Error())
	failures := pp.failureCount(email)
	escalated := quotaCooldownDuration(failures + 1)

	// Pick whichever is longer: server hint or escalated formula.
	resetDuration := serverDelay
	if escalated > resetDuration {
		resetDuration = escalated
	}

	pp.markCooldown(email, resetDuration, err)
	logPool.Logf("account %s cooldown %s (server=%s, escalated=%s) — trying next...",
		email, formatDuration(resetDuration), formatDuration(serverDelay), formatDuration(escalated))
	logPool.Logf("quota error detail: %s", truncateError(err, 300))
}

func (pp *ProviderPool) handleModelCapacityError(email string, err error) {
	cooldown := modelCapacityCooldown()
	pp.mu.Lock()
	now := time.Now()
	pp.globalCooldownUntil = now.Add(cooldown)
	for i := range pp.entries {
		next := now.Add(cooldown)
		if pp.entries[i].cooldownUntil.Before(next) {
			pp.entries[i].cooldownUntil = next
		}
	}
	queue := pp.queue
	queueEnabled := pp.queueEnabled
	pp.mu.Unlock()
	atomic.AddUint64(&pp.metricModelCapacityGlobalCooldown, 1)
	logPool.Warnf("MODEL_CAPACITY: global cooldown %s applied to all %d account(s) (triggered by %s): %s",
		formatDuration(cooldown), pp.Size(), email, truncateError(err, 200))

	// Wake up queued requests once the cooldown expires so they can retry.
	if queueEnabled && queue != nil {
		go func() {
			time.Sleep(cooldown)
			queue.notifyAll()
			logPool.Logf("queue: notified %d waiter(s) after model capacity cooldown expired", queue.depth())
		}()
	}
}

func (pp *ProviderPool) handleBillingError(email string, err error) {
	failures := pp.failureCount(email)
	duration := billingDisableDuration(failures + 1)
	pp.markDisabled(email, duration, "billing", err)
	logPool.Warnf("BILLING: account %s disabled for %s: %s", email, formatDuration(duration), truncateError(err, 200))
}

func (pp *ProviderPool) handleOverloadedError(email string, err error) {
	cooldown := overloadedCooldown()
	pp.markCooldown(email, cooldown, err)
	logPool.Warnf("OVERLOADED: account %s cooldown %s: %s", email, formatDuration(cooldown), truncateError(err, 200))
}

func (pp *ProviderPool) handleTransientError(email string, err error) {
	cooldown := transientCooldown(2*time.Second, pp.failureCount(email))
	pp.MarkTransientFailure(email, err, cooldown)
	logPool.Warnf("transient error with %s (cooldown %s): %s", email, formatDuration(cooldown), truncateError(err, 220))
}

func (pp *ProviderPool) exhaustedError(waitRetryUsed bool) error {
	pp.mu.RLock()
	earliest := pp.findEarliestResetLocked()
	totalEntries := len(pp.entries)
	pp.mu.RUnlock()

	if waitRetryUsed {
		atomic.AddUint64(&pp.metricTransientWaitRetryFailed, 1)
		logPool.Logf("metric pool.transient_wait_retry_failed=1")
	}
	if earliest != "" {
		atomic.AddUint64(&pp.metricAllAccountsExhausted, 1)
		logPool.Warnf("all %d account(s) exhausted — next reset: %s", totalEntries, earliest)
		return fmt.Errorf("all %d OAuth account(s) exhausted — next reset: %s", totalEntries, earliest)
	}
	atomic.AddUint64(&pp.metricAllAccountsExhausted, 1)
	return fmt.Errorf("all %d OAuth account(s) exhausted", totalEntries)
}
