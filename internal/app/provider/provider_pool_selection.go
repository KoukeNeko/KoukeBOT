package provider

import (
	"fmt"
	"time"
)

// selectNextProviderLocked returns the best available provider index.
// Must be called with pp.mu held.
func (pp *ProviderPool) selectNextProviderLocked(now time.Time) (int, bool) {
	if len(pp.entries) == 0 {
		return -1, false
	}

	// Global cooldown: all accounts blocked when model server has no capacity.
	if !pp.globalCooldownUntil.IsZero() && pp.globalCooldownUntil.After(now) {
		return -1, false
	}

	// Clear expired cooldowns and reset error counts before selection.
	pp.clearExpiredCooldowns(now)

	available := make([]int, 0, len(pp.entries))
	for i := range pp.entries {
		e := &pp.entries[i]
		if e.isAvailable(now) {
			available = append(available, i)
		}
	}
	if len(available) == 0 {
		return -1, false
	}

	best := available[0]
	for _, idx := range available[1:] {
		if betterCandidate(pp.entries[idx], pp.entries[best], idx, best, pp.current, len(pp.entries)) {
			best = idx
		}
	}
	return best, true
}

func betterCandidate(a, b poolEntry, idxA, idxB, current, size int) bool {
	if a.consecutiveFailures != b.consecutiveFailures {
		return a.consecutiveFailures < b.consecutiveFailures
	}

	if !a.lastUsed.Equal(b.lastUsed) {
		if a.lastUsed.IsZero() {
			return true
		}
		if b.lastUsed.IsZero() {
			return false
		}
		return a.lastUsed.Before(b.lastUsed)
	}

	return circularDistance(idxA, current, size) < circularDistance(idxB, current, size)
}

func circularDistance(idx, current, size int) int {
	if idx >= current {
		return idx - current
	}
	return size - current + idx
}

func (pp *ProviderPool) findEntryByEmailLocked(email string) *poolEntry {
	for i := range pp.entries {
		if pp.entries[i].email == email {
			return &pp.entries[i]
		}
	}
	return nil
}

func (pp *ProviderPool) shortTransientWaitLocked(now time.Time) (time.Duration, bool) {
	if !pp.transientRecoveryWaitEnabled || pp.transientRecoveryWaitMax <= 0 {
		return 0, false
	}
	earliest, _, ok := pp.findEarliestResetTimeLocked(now)
	if !ok {
		return 0, false
	}
	wait := earliest.Sub(now)
	if wait <= 0 || wait > pp.transientRecoveryWaitMax {
		return 0, false
	}
	return addJitter(wait, 250*time.Millisecond), true
}

// findEarliestResetLocked returns a human-readable string for the soonest recovery time.
// Must be called with pp.mu held.
func (pp *ProviderPool) findEarliestResetLocked() string {
	return pp.findEarliestResetLockedAt(time.Now())
}

func (pp *ProviderPool) findEarliestResetLockedAt(now time.Time) string {
	earliest, email, ok := pp.findEarliestResetTimeLocked(now)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s (%s)", formatDuration(earliest.Sub(now)), email)
}

func (pp *ProviderPool) findEarliestResetTimeLocked(now time.Time) (time.Time, string, bool) {
	var earliest time.Time
	var earliestEmail string

	for _, e := range pp.entries {
		effective := e.effectiveExhaustedUntil()
		if effective.After(now) {
			if earliest.IsZero() || effective.Before(earliest) {
				earliest = effective
				earliestEmail = e.email
			}
		}
	}

	if earliest.IsZero() {
		return time.Time{}, "", false
	}
	return earliest, earliestEmail, true
}

// clearExpiredCooldowns resets expired timers and error counts.
// Must be called with pp.mu held (write lock).
func (pp *ProviderPool) clearExpiredCooldowns(now time.Time) {
	for i := range pp.entries {
		e := &pp.entries[i]
		cooldownExpired := !e.cooldownUntil.IsZero() && !e.cooldownUntil.After(now)
		disabledExpired := !e.disabledUntil.IsZero() && !e.disabledUntil.After(now)

		if cooldownExpired {
			e.cooldownUntil = time.Time{}
		}
		if disabledExpired {
			e.disabledUntil = time.Time{}
			e.disabledReason = ""
		}

		// Reset error count only when ALL timers have cleared.
		allClear := e.cooldownUntil.IsZero() && e.disabledUntil.IsZero()
		if allClear && e.consecutiveFailures > 0 && (cooldownExpired || disabledExpired) {
			e.consecutiveFailures = 0
		}
	}
}
