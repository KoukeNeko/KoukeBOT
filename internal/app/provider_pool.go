package app

import (
	"context"
	"fmt"
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

	// Quota tracking
	requestCount   int       // requests since last reset
	lastResetTime  time.Time // when quota counter was last reset
	totalRequests  int       // lifetime request count
}

// PoolStatus is the public view of a pool entry for API responses.
type PoolStatus struct {
	Email          string     `json:"email"`
	Status         string     `json:"status"` // "available", "exhausted"
	ExhaustedUntil *time.Time `json:"exhausted_until,omitempty"`
	Current        bool       `json:"current"`

	// Quota tracking
	RequestCount   int       `json:"request_count"`    // requests since last reset
	TotalRequests  int       `json:"total_requests"`   // lifetime requests
	LastResetTime  time.Time `json:"last_reset_time"`  // when quota counter was reset
}

// ProviderPool wraps multiple llm.Provider instances with automatic 429 failover.
// It implements llm.Provider so it can be used as a drop-in replacement.
type ProviderPool struct {
	mu             sync.RWMutex
	entries        []poolEntry
	current        int // round-robin index
	lastUsedEmail  string
	tools          []domain.Tool
	autoRetryCount int // number of retries for short-delay 429s
}

// NewProviderPool creates an empty provider pool.
func NewProviderPool() *ProviderPool {
	return &ProviderPool{
		autoRetryCount: 3, // default: 3 retries for short-delay 429s
	}
}

// SetAutoRetryCount configures the number of retries for short-delay 429 errors.
func (pp *ProviderPool) SetAutoRetryCount(count int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.autoRetryCount = count
}

// Add appends a provider to the pool.
func (pp *ProviderPool) Add(provider llm.Provider, email string) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Register existing tools with the new provider
	if len(pp.tools) > 0 {
		provider.RegisterTools(pp.tools)
	}

	pp.entries = append(pp.entries, poolEntry{
		provider: provider,
		email:    email,
	})
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
		result[i] = PoolStatus{
			Email:          e.email,
			Status:         status,
			ExhaustedUntil: until,
			Current:        e.email == pp.lastUsedEmail,
			RequestCount:   e.requestCount,
			TotalRequests:  e.totalRequests,
			LastResetTime:  e.lastResetTime,
		}
	}
	return result
}

// Chat implements llm.Provider with automatic 429 failover.
func (pp *ProviderPool) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return tryPool(pp, func(p llm.Provider) (*llm.ChatResponse, error) {
		return p.Chat(ctx, req)
	})
}

// StreamChat implements llm.Provider with automatic 429 failover.
func (pp *ProviderPool) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return tryPool(pp, func(p llm.Provider) (<-chan llm.StreamChunk, error) {
		return p.StreamChat(ctx, req)
	})
}

// StreamChatMultimodal implements llm.Provider with automatic 429 failover.
func (pp *ProviderPool) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	return tryPool(pp, func(p llm.Provider) (<-chan llm.StreamChunk, error) {
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

// tryPool is a generic failover function that tries providers in round-robin order.
// On 429/RESOURCE_EXHAUSTED, it marks the provider as exhausted and tries the next one.
func tryPool[T any](pp *ProviderPool, fn func(llm.Provider) (T, error)) (T, error) {
	pp.mu.Lock()

	if len(pp.entries) == 0 {
		pp.mu.Unlock()
		var zero T
		return zero, ErrProviderNotConfigured
	}

	now := time.Now()
	tried := 0
	total := len(pp.entries)
	startIdx := pp.current

	for tried < total {
		idx := (startIdx + tried) % total
		entry := &pp.entries[idx]

		// Skip exhausted providers
		if !entry.exhaustedUntil.IsZero() && entry.exhaustedUntil.After(now) {
			tried++
			continue
		}

		// Advance round-robin for next call
		pp.current = (idx + 1) % total
		email := entry.email
		provider := entry.provider
		pp.mu.Unlock()

		result, err := fn(provider)

		// Update usage stats regardless of success/failure
		pp.mu.Lock()
		for i := range pp.entries {
			if pp.entries[i].email == email {
				pp.entries[i].requestCount++
				pp.entries[i].totalRequests++
				pp.entries[i].lastUsed = time.Now()

				// Auto-reset quota counter if it's been more than 24 hours
				if time.Since(pp.entries[i].lastResetTime) > 24*time.Hour {
					pp.entries[i].requestCount = 1 // current request
					pp.entries[i].lastResetTime = now
				}
				break
			}
		}
		pp.lastUsedEmail = email
		pp.mu.Unlock()

		if err == nil {
			return result, nil
		}

		// Classify error type
		errType := classifyError(err)

		switch errType {
		case errorQuota:
			resetDuration := parseResetDelay(err.Error())

			// Short-delay 429: wait and retry same account
			if resetDuration <= 30*time.Second {
				pp.mu.Lock()
				retryCount := pp.autoRetryCount
				pp.mu.Unlock()

				for retry := 0; retry < retryCount; retry++ {
					logPool.Logf("account %s: short delay (%s), retry %d/%d...", email, formatDuration(resetDuration), retry+1, retryCount)
					time.Sleep(resetDuration)

					result, err = fn(provider)
					if err == nil {
						pp.mu.Lock()
						pp.lastUsedEmail = email
						for i := range pp.entries {
							if pp.entries[i].email == email {
								pp.entries[i].lastUsed = time.Now()
								break
							}
						}
						pp.mu.Unlock()
						return result, nil
					}

					// Still quota error? Re-parse delay
					if classifyError(err) == errorQuota {
						resetDuration = parseResetDelay(err.Error())
						if resetDuration > 30*time.Second {
							break // Long delay: switch to next account
						}
					} else {
						break // Non-quota error: stop retrying
					}
				}
			}

			// Long-delay or retry exhausted: mark exhausted and try next account
			pp.mu.Lock()
			for i := range pp.entries {
				if pp.entries[i].email == email {
					pp.entries[i].exhaustedUntil = now.Add(resetDuration)
					break
				}
			}
			logPool.Logf("account %s exhausted (reset in %s) — trying next...", email, formatDuration(resetDuration))
			// Log raw error for debugging quota issues
			errStr := err.Error()
			if len(errStr) > 300 {
				errStr = errStr[:300] + "..."
			}
			logPool.Logf("quota error detail: %s", errStr)
			tried++
			continue

		case errorValidation:
			// Validation error: don't switch accounts (same payload will fail everywhere)
			logPool.Warnf("validation error with %s: %v", email, err)
			return result, err

		default:
			// Other errors: return immediately
			return result, err
		}
	}

	// All providers exhausted — find the soonest reset
	earliest := pp.findEarliestReset()
	pp.mu.Unlock()

	var zero T
	if earliest != "" {
		logPool.Warnf("all %d account(s) exhausted — next reset: %s", total, earliest)
		return zero, fmt.Errorf("all %d OAuth account(s) exhausted — next reset: %s", total, earliest)
	}
	return zero, fmt.Errorf("all %d OAuth account(s) exhausted", total)
}

// findEarliestReset returns a human-readable string for the soonest recovery time.
// Must be called with pp.mu held.
func (pp *ProviderPool) findEarliestReset() string {
	var earliest time.Time
	var earliestEmail string
	now := time.Now()

	for _, e := range pp.entries {
		if e.exhaustedUntil.After(now) {
			if earliest.IsZero() || e.exhaustedUntil.Before(earliest) {
				earliest = e.exhaustedUntil
				earliestEmail = e.email
			}
		}
	}

	if earliest.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s (%s)", formatDuration(earliest.Sub(now)), earliestEmail)
}

// errorType classifies errors for appropriate failover handling.
type errorType int

const (
	errorNone       errorType = iota // not an error
	errorQuota                       // 429 rate limit / RESOURCE_EXHAUSTED
	errorValidation                  // format/validation errors (don't switch accounts)
	errorOther                       // other errors (return immediately)
)

// classifyError determines the error type for failover logic.
func classifyError(err error) errorType {
	if err == nil {
		return errorNone
	}
	s := err.Error()

	// Quota/rate-limit errors → retry with next account
	if strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") {
		return errorQuota
	}

	// Validation errors → don't switch accounts (same payload will fail everywhere)
	if strings.Contains(s, "tool_use_id") ||
		strings.Contains(s, "invalid_request_error") ||
		strings.Contains(s, "validation") {
		return errorValidation
	}

	// Other errors → return immediately
	return errorOther
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
