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

	available := make([]int, 0, len(pp.entries))
	for i := range pp.entries {
		e := &pp.entries[i]
		if e.exhaustedUntil.IsZero() || !e.exhaustedUntil.After(now) {
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
		if e.exhaustedUntil.After(now) {
			if earliest.IsZero() || e.exhaustedUntil.Before(earliest) {
				earliest = e.exhaustedUntil
				earliestEmail = e.email
			}
		}
	}

	if earliest.IsZero() {
		return time.Time{}, "", false
	}
	return earliest, earliestEmail, true
}
