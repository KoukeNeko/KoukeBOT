package provider

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultQueueCapacity = 10
	defaultQueueTimeout  = 120 * time.Second
)

// Sentinel errors for queue operations.
var (
	ErrQueueFull    = fmt.Errorf("request queue full — please try again later")
	ErrQueueTimeout = fmt.Errorf("request timed out waiting in queue")
)

// requestQueue provides bounded waiting for requests when all providers
// are in cooldown (e.g. global model capacity cooldown).
type requestQueue struct {
	mu       sync.Mutex
	capacity int
	timeout  time.Duration
	waiters  []chan struct{}
	waiting  int32 // atomic counter for observability
}

func newRequestQueue(capacity int, timeout time.Duration) *requestQueue {
	if capacity <= 0 {
		capacity = defaultQueueCapacity
	}
	if timeout <= 0 {
		timeout = defaultQueueTimeout
	}
	return &requestQueue{
		capacity: capacity,
		timeout:  timeout,
	}
}

// enqueue blocks the caller until signalled or timeout/cancellation.
// Returns the caller's queue position (1-indexed) and nil on success.
func (q *requestQueue) enqueue(ctx context.Context) (position int, err error) {
	q.mu.Lock()
	if len(q.waiters) >= q.capacity {
		q.mu.Unlock()
		return 0, ErrQueueFull
	}
	ch := make(chan struct{})
	q.waiters = append(q.waiters, ch)
	pos := len(q.waiters)
	atomic.AddInt32(&q.waiting, 1)
	q.mu.Unlock()

	defer atomic.AddInt32(&q.waiting, -1)

	timer := time.NewTimer(q.timeout)
	defer timer.Stop()

	select {
	case <-ch:
		return pos, nil
	case <-timer.C:
		q.removeWaiter(ch)
		return pos, ErrQueueTimeout
	case <-ctx.Done():
		q.removeWaiter(ch)
		return pos, ctx.Err()
	}
}

// notifyAll signals all waiters that a provider may be available again.
func (q *requestQueue) notifyAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ch := range q.waiters {
		close(ch)
	}
	q.waiters = nil
}

func (q *requestQueue) removeWaiter(ch chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, w := range q.waiters {
		if w == ch {
			q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
			return
		}
	}
}

// depth returns the number of requests currently waiting in the queue.
func (q *requestQueue) depth() int {
	return int(atomic.LoadInt32(&q.waiting))
}
