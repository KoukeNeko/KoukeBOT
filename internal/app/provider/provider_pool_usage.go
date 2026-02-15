package provider

import (
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func (pp *ProviderPool) recordAttempt(email string, now time.Time, success bool) {
	var snapshot *llm.OAuthUsageStats

	pp.mu.Lock()
	entry := pp.findEntryByEmailLocked(email)
	if entry != nil {
		entry.lastUsed = now
		pp.lastUsedEmail = email
		entry.usage = updateUsageStats(entry.usage, now, success)
		snapshot = cloneUsageStats(entry.usage)
	}
	pp.mu.Unlock()

	if snapshot != nil {
		pp.persistUsageStats(email, snapshot)
	}
}

func (pp *ProviderPool) markSuccess(email string, now time.Time) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	entry := pp.findEntryByEmailLocked(email)
	if entry == nil {
		return
	}
	entry.consecutiveFailures = 0
	entry.lastError = ""
	entry.exhaustedUntil = time.Time{}
	entry.lastSuccessAt = now
}

func (pp *ProviderPool) markExhausted(email string, resetDuration time.Duration, err error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	entry := pp.findEntryByEmailLocked(email)
	if entry == nil {
		return
	}
	now := time.Now()
	entry.exhaustedUntil = now.Add(resetDuration)
	entry.consecutiveFailures++
	entry.lastError = truncateError(err, 240)
	entry.lastFailureAt = now
}

func (pp *ProviderPool) failureCount(email string) int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	for i := range pp.entries {
		if pp.entries[i].email == email {
			return pp.entries[i].consecutiveFailures
		}
	}
	return 0
}

func (pp *ProviderPool) getAutoRetryCount() int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.autoRetryCount
}

func (pp *ProviderPool) persistUsageStats(email string, usage *llm.OAuthUsageStats) {
	pp.mu.RLock()
	path := pp.poolStorePath
	pp.mu.RUnlock()
	if path == "" || usage == nil || email == "" {
		return
	}
	if err := llm.UpdateOAuthPoolUsageStats(path, email, usage); err != nil {
		logPool.Warnf("pool: persist usage failed for %s: %v", email, err)
	}
}

func cloneUsageStats(stats *llm.OAuthUsageStats) *llm.OAuthUsageStats {
	if stats == nil {
		return nil
	}
	cloned := *stats
	if len(stats.HourlyBuckets) > 0 {
		cloned.HourlyBuckets = append([]llm.OAuthUsageBucket(nil), stats.HourlyBuckets...)
	}
	return &cloned
}

func updateUsageStats(stats *llm.OAuthUsageStats, now time.Time, success bool) *llm.OAuthUsageStats {
	if stats == nil {
		stats = &llm.OAuthUsageStats{}
	}

	hour := now.Truncate(time.Hour)
	found := false
	for i := range stats.HourlyBuckets {
		if stats.HourlyBuckets[i].HourStart.Equal(hour) {
			stats.HourlyBuckets[i].Attempts++
			if success {
				stats.HourlyBuckets[i].Successes++
			} else {
				stats.HourlyBuckets[i].Failures++
			}
			found = true
			break
		}
	}
	if !found {
		b := llm.OAuthUsageBucket{HourStart: hour, Attempts: 1}
		if success {
			b.Successes = 1
		} else {
			b.Failures = 1
		}
		stats.HourlyBuckets = append(stats.HourlyBuckets, b)
	}

	stats.AttemptsTotal++
	if success {
		stats.SuccessesTotal++
	} else {
		stats.FailuresTotal++
	}
	stats.LastAttemptAt = now

	cutoff := now.Add(-24 * time.Hour)
	pruned := stats.HourlyBuckets[:0]
	for _, b := range stats.HourlyBuckets {
		if b.HourStart.After(cutoff) || b.HourStart.Equal(cutoff) {
			pruned = append(pruned, b)
		}
	}
	stats.HourlyBuckets = pruned
	return stats
}

func summarizeUsage24h(stats *llm.OAuthUsageStats, now time.Time) (attempts, successes, failures int, windowStart time.Time) {
	if stats == nil {
		return 0, 0, 0, time.Time{}
	}
	cutoff := now.Add(-24 * time.Hour)
	for _, b := range stats.HourlyBuckets {
		if !(b.HourStart.After(cutoff) || b.HourStart.Equal(cutoff)) {
			continue
		}
		attempts += b.Attempts
		successes += b.Successes
		failures += b.Failures
		if windowStart.IsZero() || b.HourStart.Before(windowStart) {
			windowStart = b.HourStart
		}
	}
	return attempts, successes, failures, windowStart
}

func summarizeUsageTotals(stats *llm.OAuthUsageStats) (attempts, successes, failures int) {
	if stats == nil {
		return 0, 0, 0
	}
	return stats.AttemptsTotal, stats.SuccessesTotal, stats.FailuresTotal
}

func usageLastAttemptAt(stats *llm.OAuthUsageStats) *time.Time {
	if stats == nil || stats.LastAttemptAt.IsZero() {
		return nil
	}
	t := stats.LastAttemptAt
	return &t
}
