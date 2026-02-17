package provider

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logPool = logger.New("llm", logger.Yellow)

// poolEntry holds a single provider with its status.
type poolEntry struct {
	provider llm.Provider
	email    string
	lastUsed time.Time

	// Dual timer: short cooldown for rate limits, long disable for billing errors.
	cooldownUntil  time.Time // Short cooldown (rate_limit/overloaded: 1m→5m→25m→1h)
	disabledUntil  time.Time // Long disable (billing/auth: 5h→10h→20h→24h)
	disabledReason string    // Why this account is disabled ("billing", "auth", etc.)

	usage *llm.OAuthUsageStats

	consecutiveFailures int
	lastError           string
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
}

// isAvailable returns true if the account is not in cooldown or disabled.
func (e *poolEntry) isAvailable(now time.Time) bool {
	if !e.disabledUntil.IsZero() && e.disabledUntil.After(now) {
		return false
	}
	if !e.cooldownUntil.IsZero() && e.cooldownUntil.After(now) {
		return false
	}
	return true
}

// effectiveExhaustedUntil returns the later of cooldownUntil and disabledUntil
// for backward compatibility with code that needs a single "exhausted until" time.
func (e *poolEntry) effectiveExhaustedUntil() time.Time {
	if e.disabledUntil.After(e.cooldownUntil) {
		return e.disabledUntil
	}
	return e.cooldownUntil
}

// PoolStatus is the public view of a pool entry for API responses.
type PoolStatus struct {
	Email          string     `json:"email"`
	Status         string     `json:"status"` // "available", "cooldown", "disabled"
	ExhaustedUntil *time.Time `json:"exhausted_until,omitempty"` // Backward compat: max(cooldownUntil, disabledUntil)
	CooldownUntil  *time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  *time.Time `json:"disabled_until,omitempty"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	Current        bool       `json:"current"`

	RequestCount   int        `json:"request_count"`
	TotalRequests  int        `json:"total_requests"`
	LastResetTime  time.Time  `json:"last_reset_time"`
	Attempts24h    int        `json:"attempts_24h"`
	Successes24h   int        `json:"successes_24h"`
	Failures24h    int        `json:"failures_24h"`
	AttemptsTotal  int        `json:"attempts_total"`
	SuccessesTotal int        `json:"successes_total"`
	FailuresTotal  int        `json:"failures_total"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`

	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastError           string     `json:"last_error,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
}

// ProviderPool wraps multiple llm.Provider instances with automatic 429 failover.
type ProviderPool struct {
	mu             sync.RWMutex
	entries        []poolEntry
	current        int
	lastUsedEmail  string
	tools          []domain.Tool
	autoRetryCount int
	poolStorePath  string

	transientRecoveryWaitEnabled bool
	transientRecoveryWaitMax     time.Duration

	// Global cooldown: blocks ALL accounts when model server has no capacity.
	globalCooldownUntil time.Time

	// Circuit breaker: trips open after repeated model capacity failures.
	circuitBreaker *circuitBreaker

	// Fallback model: tried when primary model has no capacity.
	fallbackModel string

	// Observability fields for status endpoint / UI.
	effectiveModel     string
	effectiveEndpoint  string
	lastFallbackReason string
	lastFallbackAt     time.Time

	// Request queue: buffers requests when all accounts are in cooldown.
	queue        *requestQueue
	queueEnabled bool

	metricTransientWaitRetry          uint64
	metricTransientWaitRetrySucceeded uint64
	metricTransientWaitRetryFailed    uint64
	metricAllAccountsExhausted        uint64
	metricModelCapacityGlobalCooldown uint64
	metricModelFallback               uint64
}

// NewProviderPool creates an empty provider pool.
func NewProviderPool() *ProviderPool {
	return &ProviderPool{
		autoRetryCount:               3,
		transientRecoveryWaitEnabled: true,
		transientRecoveryWaitMax:     5 * time.Second,
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

// SetTransientRecoveryWait configures short wait-and-retry behavior.
func (pp *ProviderPool) SetTransientRecoveryWait(enabled bool, maxWait time.Duration) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if maxWait <= 0 {
		maxWait = 5 * time.Second
	}
	pp.transientRecoveryWaitEnabled = enabled
	pp.transientRecoveryWaitMax = maxWait
}

// SetFallbackModel configures a fallback model to try when the primary model
// has no capacity. Empty string disables fallback.
func (pp *ProviderPool) SetFallbackModel(model string) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.fallbackModel = strings.TrimSpace(model)
}

// SetQueueConfig enables or disables the request queue that buffers incoming
// requests when all accounts are in cooldown.
func (pp *ProviderPool) SetQueueConfig(enabled bool, capacity int, timeout time.Duration) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.queueEnabled = enabled
	if enabled {
		pp.queue = newRequestQueue(capacity, timeout)
	} else {
		pp.queue = nil
	}
}

// QueueDepth returns the number of requests currently waiting in the queue.
func (pp *ProviderPool) QueueDepth() int {
	pp.mu.RLock()
	q := pp.queue
	pp.mu.RUnlock()
	if q == nil {
		return 0
	}
	return q.depth()
}

// SetCircuitBreaker enables a circuit breaker that trips open after the given
// number of consecutive model-capacity failures across all accounts, preventing
// wasteful retries for the specified duration.
func (pp *ProviderPool) SetCircuitBreaker(failureThreshold int, openDuration time.Duration) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if failureThreshold <= 0 || openDuration <= 0 {
		pp.circuitBreaker = nil
		return
	}
	pp.circuitBreaker = newCircuitBreaker(failureThreshold, openDuration)
}

type ProviderPoolMetrics struct {
	TransientWaitRetry          uint64 `json:"transient_wait_retry"`
	TransientWaitRetrySucceeded uint64 `json:"transient_wait_retry_succeeded"`
	TransientWaitRetryFailed    uint64 `json:"transient_wait_retry_failed"`
	AllAccountsExhausted        uint64 `json:"all_accounts_exhausted"`
	ModelCapacityGlobalCooldown uint64 `json:"model_capacity_global_cooldown"`
	ModelFallback               uint64 `json:"model_fallback"`
}

func (pp *ProviderPool) MetricsSnapshot() ProviderPoolMetrics {
	if pp == nil {
		return ProviderPoolMetrics{}
	}
	return ProviderPoolMetrics{
		TransientWaitRetry:          atomic.LoadUint64(&pp.metricTransientWaitRetry),
		TransientWaitRetrySucceeded: atomic.LoadUint64(&pp.metricTransientWaitRetrySucceeded),
		TransientWaitRetryFailed:    atomic.LoadUint64(&pp.metricTransientWaitRetryFailed),
		AllAccountsExhausted:        atomic.LoadUint64(&pp.metricAllAccountsExhausted),
		ModelCapacityGlobalCooldown: atomic.LoadUint64(&pp.metricModelCapacityGlobalCooldown),
		ModelFallback:               atomic.LoadUint64(&pp.metricModelFallback),
	}
}
