package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logPool = logger.New("llm", logger.Yellow)

// poolEntry holds a single provider with its status.
type poolEntry struct {
	provider       llm.Provider
	email          string
	exhaustedUntil time.Time // zero value = available
	lastUsed       time.Time // for load balancing

	// Usage tracking (persisted to oauth_pool.json)
	usage *llm.OAuthUsageStats

	// Health tracking
	consecutiveFailures int
	lastError           string
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
}

// PoolStatus is the public view of a pool entry for API responses.
type PoolStatus struct {
	Email          string     `json:"email"`
	Status         string     `json:"status"` // "available", "exhausted"
	ExhaustedUntil *time.Time `json:"exhausted_until,omitempty"`
	Current        bool       `json:"current"`

	// Usage tracking
	RequestCount   int        `json:"request_count"`   // legacy alias: attempts_24h
	TotalRequests  int        `json:"total_requests"`  // legacy alias: attempts_total
	LastResetTime  time.Time  `json:"last_reset_time"` // legacy field, no longer quota-reset time
	Attempts24h    int        `json:"attempts_24h"`
	Successes24h   int        `json:"successes_24h"`
	Failures24h    int        `json:"failures_24h"`
	AttemptsTotal  int        `json:"attempts_total"`
	SuccessesTotal int        `json:"successes_total"`
	FailuresTotal  int        `json:"failures_total"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`

	// Health tracking
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastError           string     `json:"last_error,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
}

// ProviderPool wraps multiple llm.Provider instances with automatic 429 failover.
// It implements llm.Provider so it can be used as a drop-in replacement.
type ProviderPool struct {
	mu             sync.RWMutex
	entries        []poolEntry
	current        int // weighted round-robin pointer
	lastUsedEmail  string
	tools          []domain.Tool
	autoRetryCount int // number of retries for short-delay 429s
	poolStorePath  string

	transientRecoveryWaitEnabled bool
	transientRecoveryWaitMax     time.Duration
}

// NewProviderPool creates an empty provider pool.
func NewProviderPool() *ProviderPool {
	return &ProviderPool{
		autoRetryCount:               3,               // default: 3 retries for short-delay 429s
		transientRecoveryWaitEnabled: true,            // default: wait-and-retry when all accounts are cooling down briefly
		transientRecoveryWaitMax:     5 * time.Second, // default max wait budget
	}
}

// SetAutoRetryCount configures the number of retries for short-delay 429 errors.
func (pp *ProviderPool) SetAutoRetryCount(count int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if count < 0 {
		count = 0
	}
	pp.autoRetryCount = count
}

// SetPoolStorePath sets the OAuth pool JSON path used for persisting usage stats.
func (pp *ProviderPool) SetPoolStorePath(path string) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.poolStorePath = strings.TrimSpace(path)
}

// SetTransientRecoveryWait configures short wait-and-retry behavior when all
// accounts are transiently cooling down.
func (pp *ProviderPool) SetTransientRecoveryWait(enabled bool, maxWait time.Duration) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if maxWait <= 0 {
		maxWait = 5 * time.Second
	}
	pp.transientRecoveryWaitEnabled = enabled
	pp.transientRecoveryWaitMax = maxWait
}

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

	// Register existing tools with the provider immediately.
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
// Selection policy: available first, healthier accounts first, then light round-robin.
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

// Chat implements llm.Provider with automatic 429 failover.
func (pp *ProviderPool) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return tryPool(ctx, pp, func(p llm.Provider) (*llm.ChatResponse, error) {
		return p.Chat(ctx, req)
	})
}

// StreamChat implements llm.Provider with automatic 429 failover.
func (pp *ProviderPool) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return tryPool(ctx, pp, func(p llm.Provider) (<-chan llm.StreamChunk, error) {
		return p.StreamChat(ctx, req)
	})
}

// StreamChatMultimodal implements llm.Provider with automatic 429 failover.
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

// RegisterTools registers tools with ALL providers in the pool.
func (pp *ProviderPool) RegisterTools(tools []domain.Tool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pp.tools = tools
	for _, e := range pp.entries {
		e.provider.RegisterTools(tools)
	}
}

// tryPool is a generic failover function.
// Strategy: available-first selection, health-aware rotation, quota-aware failover.
func tryPool[T any](ctx context.Context, pp *ProviderPool, fn func(llm.Provider) (T, error)) (T, error) {
	var zero T

	pp.mu.RLock()
	total := len(pp.entries)
	pp.mu.RUnlock()
	if total == 0 {
		return zero, ErrProviderNotConfigured
	}

	waitRetryUsed := false

	for tried := 0; tried < total; tried++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return zero, err
			}
		}

		provider, email, ok := pp.SelectNextProvider()
		if !ok {
			now := time.Now()
			pp.mu.RLock()
			waitDuration, canWait := pp.shortTransientWaitLocked(now)
			earliest := pp.findEarliestResetLockedAt(now)
			totalEntries := len(pp.entries)
			pp.mu.RUnlock()

			if canWait && !waitRetryUsed {
				waitRetryUsed = true
				logPool.Logf("metric pool.transient_wait_retry=1")
				logPool.Warnf("all %d account(s) cooling down transiently — waiting %s before one retry", totalEntries, formatDuration(waitDuration))

				timer := time.NewTimer(waitDuration)
				if ctx != nil {
					select {
					case <-timer.C:
					case <-ctx.Done():
						if !timer.Stop() {
							<-timer.C
						}
						return zero, ctx.Err()
					}
				} else {
					<-timer.C
				}
				tried--
				continue
			}

			if waitRetryUsed {
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			if earliest != "" {
				logPool.Warnf("all %d account(s) exhausted — next reset: %s", totalEntries, earliest)
				return zero, fmt.Errorf("all %d OAuth account(s) exhausted — next reset: %s", totalEntries, earliest)
			}
			return zero, fmt.Errorf("all %d OAuth account(s) exhausted", totalEntries)
		}

		result, err := fn(provider)
		pp.recordAttempt(email, time.Now(), err == nil)

		if err == nil {
			pp.markSuccess(email, time.Now())
			if waitRetryUsed {
				logPool.Logf("metric pool.transient_wait_retry_succeeded=1")
			}
			return result, nil
		}
		if isContextTerminated(err) {
			// Request-level timeout/cancel is not account-specific.
			// Do not mark provider exhausted or fail over to another account.
			if waitRetryUsed {
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err
		}

		errType := classifyError(err)
		switch errType {
		case errorQuota:
			resetDuration := parseResetDelay(err.Error())

			// Short-delay quota errors: retry same account with exponential backoff + jitter.
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
						return result, nil
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

			pp.markExhausted(email, resetDuration, err)
			logPool.Logf("account %s exhausted (reset in %s) — trying next...", email, formatDuration(resetDuration))
			logPool.Logf("quota error detail: %s", truncateError(err, 300))
			continue

		case errorTransient:
			cooldown := transientCooldown(2*time.Second, pp.failureCount(email))
			pp.MarkTransientFailure(email, err, cooldown)
			logPool.Warnf("transient error with %s (cooldown %s): %s", email, formatDuration(cooldown), truncateError(err, 220))
			continue

		case errorValidation:
			logPool.Warnf("validation error with %s: %v", email, err)
			if waitRetryUsed {
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err

		default:
			if waitRetryUsed {
				logPool.Logf("metric pool.transient_wait_retry_failed=1")
			}
			return result, err
		}
	}

	pp.mu.RLock()
	earliest := pp.findEarliestResetLocked()
	totalEntries := len(pp.entries)
	pp.mu.RUnlock()

	if waitRetryUsed {
		logPool.Logf("metric pool.transient_wait_retry_failed=1")
	}
	if earliest != "" {
		logPool.Warnf("all %d account(s) exhausted — next reset: %s", totalEntries, earliest)
		return zero, fmt.Errorf("all %d OAuth account(s) exhausted — next reset: %s", totalEntries, earliest)
	}
	return zero, fmt.Errorf("all %d OAuth account(s) exhausted", totalEntries)
}

// selectNextProviderLocked returns the best available provider index.
// Must be called with pp.mu held.
func (pp *ProviderPool) selectNextProviderLocked(now time.Time) (int, bool) {
	if len(pp.entries) == 0 {
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

// errorType classifies errors for appropriate failover handling.
type errorType int

const (
	errorNone       errorType = iota // not an error
	errorQuota                       // 429 rate limit / RESOURCE_EXHAUSTED
	errorValidation                  // format/validation errors (don't switch accounts)
	errorTransient                   // transient server/network failures
	errorOther                       // other errors (return immediately)
)

// classifyError determines the error type for failover logic.
func classifyError(err error) errorType {
	if err == nil {
		return errorNone
	}
	s := err.Error()
	lower := strings.ToLower(s)

	// Quota/rate-limit errors → retry with next account
	if strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") {
		return errorQuota
	}

	// Validation errors → don't switch accounts (same payload will fail everywhere)
	if strings.Contains(lower, "tool_use_id") ||
		strings.Contains(lower, "invalid_request_error") ||
		strings.Contains(lower, "validation") {
		return errorValidation
	}

	// Transient failures → switch account with short cooldown
	if strings.Contains(lower, "http 5") ||
		strings.Contains(lower, "internal server error") ||
		strings.Contains(lower, "bad gateway") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "gateway timeout") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "temporary") ||
		strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(s, "EOF") {
		return errorTransient
	}

	// Other errors → return immediately
	return errorOther
}

func isContextTerminated(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "context canceled")
}

// isQuotaError checks if an error indicates a 429/RESOURCE_EXHAUSTED response.
func isQuotaError(err error) bool {
	return classifyError(err) == errorQuota
}

// quotaResetDelayRe matches "quotaResetDelay":"123s" in JSON error bodies.
var quotaResetDelayRe = regexp.MustCompile(`"quotaResetDelay"\s*:\s*"(\d+)s"`)

// humanResetDelayRe matches "reset after 1h38m34s" or "reset in 5m" in error messages.
// Captures the duration string (e.g., "1h38m34s", "5m", "2h").
var humanResetDelayRe = regexp.MustCompile(`reset (?:after|in) ((?:\d+h)?(?:\d+m)?(?:\d+s)?)`)

// parseResetDelay extracts the quota reset duration from an error message.
// Supports:
//  1. JSON format: "quotaResetDelay":"123s"
//  2. Human-readable: "reset after 1h38m34s" or "reset in 5m"
//  3. "No capacity available" → 30 seconds (short retry for transient overload)
//
// Falls back to 2 minutes if unparseable.
func parseResetDelay(errMsg string) time.Duration {
	// Try JSON format first
	matches := quotaResetDelayRe.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		seconds, err := strconv.Atoi(matches[1])
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	// Try human-readable format (e.g., "reset after 1h38m34s")
	matches = humanResetDelayRe.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		if d, err := time.ParseDuration(matches[1]); err == nil && d > 0 {
			return d
		}
	}

	// Special case: "No capacity available" → short retry (transient overload)
	if strings.Contains(errMsg, "No capacity available") {
		return 30 * time.Second
	}

	// Default: 2 minutes (short enough to recover from per-minute rate limits)
	return 2 * time.Minute
}

func transientCooldown(base time.Duration, failureCount int) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if failureCount < 1 {
		failureCount = 1
	}
	cooldown := base * time.Duration(1<<(failureCount-1))
	if cooldown > 30*time.Second {
		cooldown = 30 * time.Second
	}
	return addJitter(cooldown, 250*time.Millisecond)
}

func exponentialBackoff(base time.Duration, retry int) time.Duration {
	if base <= 0 {
		base = 1 * time.Second
	}
	if retry < 0 {
		retry = 0
	}
	delay := base * time.Duration(1<<retry)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return addJitter(delay, 500*time.Millisecond)
}

func addJitter(base, maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(maxJitter)))
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

	// Keep 24h rolling window using hourly buckets.
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
		b := llm.OAuthUsageBucket{
			HourStart: hour,
			Attempts:  1,
		}
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

func truncateError(err error, maxLen int) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatDuration returns a compact human-readable duration (e.g., "6h22m").
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
