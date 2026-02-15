package api

import (
	"net/http"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
)

type RuntimeMetricsResponse struct {
	Timestamp string `json:"timestamp"`

	ToolGuardDuplicateBlocked uint64 `json:"tool_guard_duplicate_blocked"`
	ToolGuardLimitBlocked     uint64 `json:"tool_guard_limit_blocked"`
	ToolGuardMaxRounds        uint64 `json:"tool_guard_max_rounds"`
	ToolForcedFinalSuccess    uint64 `json:"tool_forced_final_success"`
	ToolForcedFinalFailed     uint64 `json:"tool_forced_final_failed"`
	ToolLoopFallback          uint64 `json:"tool_loop_fallback"`

	PoolTransientWaitRetry          uint64 `json:"pool_transient_wait_retry"`
	PoolTransientWaitRetrySucceeded uint64 `json:"pool_transient_wait_retry_succeeded"`
	PoolTransientWaitRetryFailed    uint64 `json:"pool_transient_wait_retry_failed"`
	PoolAllAccountsExhausted        uint64 `json:"pool_all_accounts_exhausted"`
	PoolAccountsTotal               int    `json:"pool_accounts_total"`
	PoolAccountsExhaustedCurrent    int    `json:"pool_accounts_exhausted_current"`
}

func (h *Handlers) GetRuntimeMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot := app.BuildRuntimeMetricsSnapshot(h.chatService, h.providerManager)

	writeJSON(w, http.StatusOK, RuntimeMetricsResponse{
		Timestamp: snapshot.Timestamp.Format(time.RFC3339),

		ToolGuardDuplicateBlocked: snapshot.ToolLoop.ToolGuardDuplicateBlocked,
		ToolGuardLimitBlocked:     snapshot.ToolLoop.ToolGuardLimitBlocked,
		ToolGuardMaxRounds:        snapshot.ToolLoop.ToolGuardMaxRounds,
		ToolForcedFinalSuccess:    snapshot.ToolLoop.ToolForcedFinalSuccess,
		ToolForcedFinalFailed:     snapshot.ToolLoop.ToolForcedFinalFailed,
		ToolLoopFallback:          snapshot.ToolLoop.ToolLoopFallback,

		PoolTransientWaitRetry:          snapshot.PoolTransientWaitRetry,
		PoolTransientWaitRetrySucceeded: snapshot.PoolTransientWaitRetrySucceeded,
		PoolTransientWaitRetryFailed:    snapshot.PoolTransientWaitRetryFailed,
		PoolAllAccountsExhausted:        snapshot.PoolAllAccountsExhausted,
		PoolAccountsTotal:               snapshot.PoolAccountsTotal,
		PoolAccountsExhaustedCurrent:    snapshot.PoolAccountsExhaustedCurrent,
	})
}
