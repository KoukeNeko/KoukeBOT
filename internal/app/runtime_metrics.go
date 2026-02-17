package app

import "time"

// RuntimeMetrics aggregates chat and provider-pool counters for API consumption.
type RuntimeMetrics struct {
	Timestamp time.Time

	ToolLoop ToolLoopMetrics

	PoolTransientWaitRetry          uint64
	PoolTransientWaitRetrySucceeded uint64
	PoolTransientWaitRetryFailed    uint64
	PoolAllAccountsExhausted        uint64
	PoolModelFallback               uint64
	PoolAccountsTotal               int
	PoolAccountsExhaustedCurrent    int
}

func BuildRuntimeMetricsSnapshot(chat *ChatService, providerManager *ProviderManager) RuntimeMetrics {
	result := RuntimeMetrics{Timestamp: time.Now().UTC()}
	if chat != nil {
		result.ToolLoop = chat.ToolLoopMetricsSnapshot()
	}
	if providerManager == nil {
		return result
	}

	pool := providerManager.GetPool()
	if pool == nil {
		return result
	}

	poolMetrics := pool.MetricsSnapshot()
	entries := pool.Entries()

	result.PoolTransientWaitRetry = poolMetrics.TransientWaitRetry
	result.PoolTransientWaitRetrySucceeded = poolMetrics.TransientWaitRetrySucceeded
	result.PoolTransientWaitRetryFailed = poolMetrics.TransientWaitRetryFailed
	result.PoolAllAccountsExhausted = poolMetrics.AllAccountsExhausted
	result.PoolModelFallback = poolMetrics.ModelFallback
	result.PoolAccountsTotal = len(entries)

	exhausted := 0
	for _, entry := range entries {
		if entry.Status == "exhausted" {
			exhausted++
		}
	}
	result.PoolAccountsExhaustedCurrent = exhausted

	return result
}
