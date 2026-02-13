package app

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logCtx = logger.New("context", logger.Yellow)

var keywordPattern = regexp.MustCompile(`[A-Za-z0-9_]{3,}|[\p{Han}]{2,}`)

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
	removed := conv.Messages[:removedCount]

	if digest, ok := cm.buildDigest(removed); ok {
		conv.AddDigest(digest)
	}

	conv.Messages = conv.Messages[removedCount:]
	conv.CumulativeTokens = 0 // Reset — next turn will rebuild accurate count
	logCtx.Logf("compacted: removed %d old messages, kept %d recent, digests=%d", removedCount, keepRecent, len(conv.ContextDigests))
}

func (cm *ContextManager) buildDigest(messages []domain.Message) (domain.ConversationDigest, bool) {
	if len(messages) == 0 {
		return domain.ConversationDigest{}, false
	}

	var summaryLines []string
	var facts, preferences, openItems, decisions []string
	allText := make([]string, 0, len(messages))

	// Focus on the tail segment of the removed block (most relevant before compaction).
	start := 0
	if len(messages) > 24 {
		start = len(messages) - 24
	}
	for _, msg := range messages[start:] {
		text := normalizeText(msg.Content)
		if text == "" {
			continue
		}
		allText = append(allText, text)

		if len(summaryLines) < 10 {
			role := "A"
			if msg.Role == domain.RoleUser {
				role = "U"
			}
			summaryLines = append(summaryLines, fmt.Sprintf("- [%s] %s", role, truncate(text, 220)))
		}

		if msg.Role == domain.RoleUser {
			lower := strings.ToLower(text)
			switch {
			case containsAny(lower, "我喜歡", "我偏好", "prefer", "like", "通常", "習慣"):
				preferences = appendUnique(preferences, truncate(text, 180), 8)
			case containsAny(lower, "我叫", "我是", "i am", "my name", "我在", "我用"):
				facts = appendUnique(facts, truncate(text, 180), 8)
			}

			if containsAny(lower, "待辦", "todo", "要做", "需要", "幫我", "請你", "follow up") {
				openItems = appendUnique(openItems, truncate(text, 180), 10)
			}
			if containsAny(lower, "決定", "改成", "選擇", "we will", "let's", "採用") {
				decisions = appendUnique(decisions, truncate(text, 180), 10)
			}
		}
	}

	if len(summaryLines) == 0 {
		return domain.ConversationDigest{}, false
	}

	keywords := extractTopKeywords(strings.Join(allText, "\n"), 16)
	digest := domain.ConversationDigest{
		ID:          fmt.Sprintf("digest-%d", time.Now().UnixNano()),
		Summary:     strings.Join(summaryLines, "\n"),
		Facts:       facts,
		Preferences: preferences,
		OpenItems:   openItems,
		Decisions:   decisions,
		Keywords:    keywords,
		CreatedAt:   time.Now(),
	}
	return digest, true
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func appendUnique(arr []string, value string, limit int) []string {
	if value == "" {
		return arr
	}
	for _, v := range arr {
		if v == value {
			return arr
		}
	}
	arr = append(arr, value)
	if limit > 0 && len(arr) > limit {
		return arr[:limit]
	}
	return arr
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func extractTopKeywords(text string, max int) []string {
	if text == "" || max <= 0 {
		return nil
	}

	stopwords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true,
		"this": true, "you": true, "your": true, "from": true, "have": true,
		"please": true, "what": true, "when": true, "where": true,
		"是": true, "你": true, "我": true, "他": true, "她": true,
		"我們": true, "你們": true, "以及": true, "然後": true, "這個": true,
	}

	counts := map[string]int{}
	for _, tok := range keywordPattern.FindAllString(strings.ToLower(text), -1) {
		if stopwords[tok] {
			continue
		}
		if len(tok) < 2 {
			continue
		}
		counts[tok]++
	}

	type item struct {
		k string
		v int
	}
	items := make([]item, 0, len(counts))
	for k, v := range counts {
		items = append(items, item{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})

	out := make([]string, 0, max)
	for _, it := range items {
		out = append(out, it.k)
		if len(out) >= max {
			break
		}
	}
	return out
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
