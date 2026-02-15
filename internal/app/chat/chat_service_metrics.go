package chat

import (
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

type ToolLoopMetrics struct {
	ToolGuardDuplicateBlocked uint64 `json:"tool_guard_duplicate_blocked"`
	ToolGuardLimitBlocked     uint64 `json:"tool_guard_limit_blocked"`
	ToolGuardMaxRounds        uint64 `json:"tool_guard_max_rounds"`
	ToolForcedFinalSuccess    uint64 `json:"tool_forced_final_success"`
	ToolForcedFinalFailed     uint64 `json:"tool_forced_final_failed"`
	ToolLoopFallback          uint64 `json:"tool_loop_fallback"`
}

func (s *ChatService) ToolLoopMetricsSnapshot() ToolLoopMetrics {
	if s == nil {
		return ToolLoopMetrics{}
	}
	return ToolLoopMetrics{
		ToolGuardDuplicateBlocked: atomic.LoadUint64(&s.metricToolGuardDuplicateBlocked),
		ToolGuardLimitBlocked:     atomic.LoadUint64(&s.metricToolGuardLimitBlocked),
		ToolGuardMaxRounds:        atomic.LoadUint64(&s.metricToolGuardMaxRounds),
		ToolForcedFinalSuccess:    atomic.LoadUint64(&s.metricToolForcedFinalSuccess),
		ToolForcedFinalFailed:     atomic.LoadUint64(&s.metricToolForcedFinalFailed),
		ToolLoopFallback:          atomic.LoadUint64(&s.metricToolLoopFallback),
	}
}

func (s *ChatService) applyTokenAccounting(conv *domain.Conversation, turnTokens int) {
	if conv == nil {
		return
	}
	conv.AddUsageTokens(turnTokens)
	conv.CumulativeTokens = EstimateConversationContextTokens(conv)

	if turnTokens > 0 {
		logChat.Logf("tokens: +%d (usage_total: %d, context_estimate: %d)", turnTokens, conv.UsageTokensTotal, conv.CumulativeTokens)
	}
}
