package provider

import (
	"fmt"
	"sync"
	"time"
)

type circuitState int

const (
	circuitClosed   circuitState = iota // Normal operation
	circuitOpen                         // Rejecting all requests
	circuitHalfOpen                     // Allowing one probe request
)

const (
	defaultCircuitFailureThreshold = 5
	defaultCircuitOpenDuration     = 60 * time.Second
)

// ErrCircuitOpen is returned when the circuit breaker is open and rejecting requests.
var ErrCircuitOpen = fmt.Errorf("circuit breaker open — model temporarily unavailable, please try again later")

// circuitBreaker implements a simple state machine that trips open after
// consecutive failures, preventing wasteful retries until a probe succeeds.
type circuitBreaker struct {
	mu               sync.Mutex
	state            circuitState
	consecutiveTotal int
	failureThreshold int
	openDuration     time.Duration
	openedAt         time.Time
}

func newCircuitBreaker(failureThreshold int, openDuration time.Duration) *circuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = defaultCircuitFailureThreshold
	}
	if openDuration <= 0 {
		openDuration = defaultCircuitOpenDuration
	}
	return &circuitBreaker{
		state:            circuitClosed,
		failureThreshold: failureThreshold,
		openDuration:     openDuration,
	}
}

// allowRequest returns true if the request should be attempted.
func (cb *circuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if now.Sub(cb.openedAt) >= cb.openDuration {
			cb.state = circuitHalfOpen
			return true
		}
		return false
	case circuitHalfOpen:
		// Only one probe at a time; reject concurrent requests.
		return false
	}
	return false
}

// recordSuccess resets the circuit to closed state.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = circuitClosed
	cb.consecutiveTotal = 0
}

// recordFailure increments the failure count and opens the circuit
// when the threshold is reached.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveTotal++
	if cb.consecutiveTotal >= cb.failureThreshold {
		cb.state = circuitOpen
		cb.openedAt = time.Now()
	}
}
