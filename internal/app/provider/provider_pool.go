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
	provider       llm.Provider
	email          string
	exhaustedUntil time.Time
	lastUsed       time.Time

	usage *llm.OAuthUsageStats

	consecutiveFailures int
	lastError           string
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
}

// PoolStatus is the public view of a pool entry for API responses.
type PoolStatus struct {
	Email          string     `json:"email"`
	Status         string     `json:"status"`
	ExhaustedUntil *time.Time `json:"exhausted_until,omitempty"`
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

	metricTransientWaitRetry          uint64
	metricTransientWaitRetrySucceeded uint64
	metricTransientWaitRetryFailed    uint64
	metricAllAccountsExhausted        uint64
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

type ProviderPoolMetrics struct {
	TransientWaitRetry          uint64 `json:"transient_wait_retry"`
	TransientWaitRetrySucceeded uint64 `json:"transient_wait_retry_succeeded"`
	TransientWaitRetryFailed    uint64 `json:"transient_wait_retry_failed"`
	AllAccountsExhausted        uint64 `json:"all_accounts_exhausted"`
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
	}
}
