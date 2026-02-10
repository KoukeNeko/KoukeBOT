package app

import (
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logCtx = logger.New("context", logger.Yellow)

// ContextManager tracks token usage and triggers pre-compaction memory flush.
// Implements OpenClaw's approach: when approaching context window limit,
// inject a silent turn asking the agent to save memories before compacting history.
type ContextManager struct {
	flushThreshold int // Tokens before limit to trigger flush (default: 4000)
	windowSize     int // Total context window size (default: 800000)
}

func NewContextManager(flushThreshold, windowSize int) *ContextManager {
	if flushThreshold <= 0 {
		flushThreshold = 4000
	}
	if windowSize <= 0 {
		windowSize = 800_000 // Conservative for Gemini 2.5 Pro (actual: 1M)
	}
	return &ContextManager{
		flushThreshold: flushThreshold,
		windowSize:     windowSize,
	}
}

// ShouldFlush returns true when cumulative tokens approach the context window limit.
func (cm *ContextManager) ShouldFlush(conv *domain.Conversation) bool {
	threshold := cm.windowSize - cm.flushThreshold
	if conv.CumulativeTokens >= threshold {
		logCtx.Logf("approaching limit: %d tokens (threshold: %d)", conv.CumulativeTokens, threshold)
		return true
	}
	return false
}

// CompactHistory removes old messages, keeping only the most recent ones.
// Should be called AFTER memory flush so important information is already persisted.
func (cm *ContextManager) CompactHistory(conv *domain.Conversation) {
	const keepRecent = 20
	if len(conv.Messages) <= keepRecent {
		return
	}
	removedCount := len(conv.Messages) - keepRecent
	conv.Messages = conv.Messages[removedCount:]
	conv.CumulativeTokens = 0 // Reset — next turn will rebuild accurate count
	logCtx.Logf("compacted: removed %d old messages, kept %d recent", removedCount, keepRecent)
}
