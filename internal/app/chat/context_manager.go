package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
	"github.com/KoukeNeko/KoukeBOT/internal/util"
)

var logCtx = logger.New("context", logger.Yellow)

var keywordPattern = regexp.MustCompile(`[A-Za-z0-9_]{3,}|[\p{Han}]{2,}`)
var punctuationPattern = regexp.MustCompile(`[!?！？]{2,}`)

var digestStyleFieldCount uint64
var digestStyleQuoteCount uint64

// ContextManager tracks prompt context footprint and triggers pre-compaction memory flush.
// Implements OpenClaw's approach: when approaching context window limit,
// inject a silent turn asking the agent to save memories before compacting history.
type ContextManager struct {
	mu                   sync.RWMutex
	flushThreshold       int // Tokens before limit to trigger flush (default: 4000)
	windowSize           int // Total context window size (default: 800000)
	keepRecentUserTurns  int // Number of most recent user turns to retain during compaction
}

func NewContextManager(flushThreshold, windowSize int) *ContextManager {
	if flushThreshold <= 0 {
		flushThreshold = 4000
	}
	if windowSize <= 0 {
		windowSize = 800_000 // Conservative for Gemini 2.5 Pro (actual: 1M)
	}
	return &ContextManager{
		flushThreshold:      flushThreshold,
		windowSize:          windowSize,
		keepRecentUserTurns: defaultMaxUserTurns,
	}
}

// ShouldFlush returns true when estimated context tokens approach the context window limit.
func (cm *ContextManager) ShouldFlush(conv *domain.Conversation) bool {
	if conv == nil {
		return false
	}
	cm.mu.RLock()
	threshold := cm.windowSize - cm.flushThreshold
	cm.mu.RUnlock()
	if conv.CumulativeTokens >= threshold {
		logCtx.Logf("approaching limit: %d tokens (threshold: %d)", conv.CumulativeTokens, threshold)
		return true
	}
	return false
}

func (cm *ContextManager) UpdateKeepRecentUserTurns(v int) {
	if cm == nil {
		return
	}
	cm.mu.Lock()
	cm.keepRecentUserTurns = normalizeMaxUserTurns(v)
	cm.mu.Unlock()
}

func (cm *ContextManager) KeepRecentUserTurns() int {
	if cm == nil {
		return defaultMaxUserTurns
	}
	cm.mu.RLock()
	v := cm.keepRecentUserTurns
	cm.mu.RUnlock()
	return normalizeMaxUserTurns(v)
}

// CompactHistory removes old messages, keeping only the most recent ones.
// Should be called AFTER memory flush so important information is already persisted.
func (cm *ContextManager) CompactHistory(conv *domain.Conversation, persona *domain.PersonaModule) {
	if cm == nil {
		return
	}
	cm.CompactHistoryWithDigest(conv, persona, nil)
}

// CompactHistoryWithDigest removes old messages and uses a pre-built digest if provided.
// If preBuiltDigest is nil, falls back to rule-based digest generation.
func (cm *ContextManager) CompactHistoryWithDigest(conv *domain.Conversation, persona *domain.PersonaModule, preBuiltDigest *domain.ConversationDigest) {
	keepRecent := cm.KeepRecentUserTurns()
	removedCount, addedDigests := compactHistoryWithDigest(conv, persona, keepRecent, preBuiltDigest)
	if removedCount == 0 {
		return
	}
	source := "rule-based"
	if preBuiltDigest != nil {
		source = "llm"
	}
	logCtx.Logf(
		"compacted: removed=%d kept=%d added_digests=%d digests_total=%d source=%s",
		removedCount,
		keepRecent,
		addedDigests,
		len(conv.ContextDigests),
		source,
	)
}

// EstimateConversationContextTokens estimates current context footprint from
// recent conversation messages + retained digests. This is used for flush tracking.
func EstimateConversationContextTokens(conv *domain.Conversation) int {
	return EstimateConversationContextTokensWithRecentCap(conv, defaultMaxUserTurns)
}

// EstimateConversationContextTokensWithRecentCap estimates tokens for the last
// N user turns (turn-based: each user turn includes associated assistant/tool messages).
func EstimateConversationContextTokensWithRecentCap(conv *domain.Conversation, maxUserTurns int) int {
	if conv == nil {
		return 0
	}

	maxUserTurns = normalizeMaxUserTurns(maxUserTurns)
	const maxDigests = 4
	const perMessageOverhead = 12
	const perDigestOverhead = 24
	const perImageRefOverhead = 512

	total := 0

	start := findTurnCutoffByUserTurns(conv.Messages, maxUserTurns)
	for _, msg := range conv.Messages[start:] {
		if msg.Content != "" {
			total += estimateTokens(msg.Content)
		}
		total += perMessageOverhead
		if len(msg.ImageRefs) > 0 {
			total += len(msg.ImageRefs) * perImageRefOverhead
		}
	}

	digestStart := 0
	if len(conv.ContextDigests) > maxDigests {
		digestStart = len(conv.ContextDigests) - maxDigests
	}
	for _, digest := range conv.ContextDigests[digestStart:] {
		total += perDigestOverhead
		total += estimateTokens(digest.Summary)
		total += estimateStringSliceTokens(digest.Facts)
		total += estimateStringSliceTokens(digest.Preferences)
		total += estimateStringSliceTokens(digest.OpenItems)
		total += estimateStringSliceTokens(digest.Decisions)
		total += estimateStringSliceTokens(digest.StyleQuotes)
	}

	if total < 0 {
		return 0
	}
	return total
}

// findTurnCutoffByUserTurns scans messages backward, counting genuine user turns.
// Internal tool result messages (stored with user role) are not counted as turns.
// Returns the index where the last N user turns begin (includes all associated
// assistant/tool messages for those turns).
func findTurnCutoffByUserTurns(messages []domain.Message, maxUserTurns int) int {
	if maxUserTurns <= 0 || len(messages) == 0 {
		return len(messages)
	}
	userTurnCount := 0
	lastUserIdx := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != domain.RoleUser {
			continue
		}
		// Skip internal tool results stored as user-role messages.
		if isInternalToolHistoryMessage(msg.Content) {
			continue
		}
		userTurnCount++
		if userTurnCount > maxUserTurns {
			return lastUserIdx
		}
		lastUserIdx = i
	}
	return 0
}

// compactHistoryWithKeepRecent removes old messages keeping the last N user turns
// and their associated responses. Returns how many messages were removed.
func compactHistoryWithKeepRecent(conv *domain.Conversation, persona *domain.PersonaModule, keepRecentUserTurns int) (removedCount int, addedDigests int) {
	return compactHistoryWithDigest(conv, persona, keepRecentUserTurns, nil)
}

// compactHistoryWithDigest removes old messages and uses a pre-built digest if provided.
// If preBuiltDigest is nil, falls back to rule-based digest generation.
func compactHistoryWithDigest(conv *domain.Conversation, persona *domain.PersonaModule, keepRecentUserTurns int, preBuiltDigest *domain.ConversationDigest) (removedCount int, addedDigests int) {
	if conv == nil {
		return 0, 0
	}
	keepRecentUserTurns = normalizeMaxUserTurns(keepRecentUserTurns)
	cutoff := findTurnCutoffByUserTurns(conv.Messages, keepRecentUserTurns)
	if cutoff == 0 {
		conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)
		return 0, 0
	}

	removedCount = cutoff
	removed := conv.Messages[:removedCount]

	if preBuiltDigest != nil {
		conv.AddDigest(*preBuiltDigest)
		addedDigests = 1
	} else if digest, ok := (&ContextManager{}).buildDigest(removed, persona); ok {
		conv.AddDigest(digest)
		addedDigests = 1
	}

	conv.Messages = conv.Messages[removedCount:]
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)
	return removedCount, addedDigests
}

func estimateStringSliceTokens(items []string) int {
	total := 0
	for _, item := range items {
		total += estimateTokens(item)
	}
	return total
}

func (cm *ContextManager) buildDigest(messages []domain.Message, persona *domain.PersonaModule) (domain.ConversationDigest, bool) {
	if len(messages) == 0 {
		return domain.ConversationDigest{}, false
	}

	var summaryLines []string
	var facts, preferences, openItems, decisions []string
	allText := make([]string, 0, len(messages))
	styleLexicon := extractPersonaStyleLexicon(persona)
	styleMarkersSet := map[string]bool{}
	signatureSet := map[string]bool{}
	type scoredQuote struct {
		text  string
		score int
	}
	var quotes []scoredQuote
	var criticalScore, directiveScore, playfulScore, formalScore int
	var coldScore, irritatedScore, composedScore int

	// Focus on the tail segment of the removed block (most relevant before compaction).
	const digestTailSegmentSize = 24
	start := 0
	if len(messages) > digestTailSegmentSize {
		start = len(messages) - digestTailSegmentSize
	}
	for _, msg := range messages[start:] {
		text := normalizeText(humanizeConversationTextForDigest(msg.Content))
		if text == "" {
			continue
		}
		lower := strings.ToLower(text)
		allText = append(allText, text)

		if len(summaryLines) < 10 {
			role := "A"
			if msg.Role == domain.RoleUser {
				role = "U"
			}
			summaryLines = append(summaryLines, fmt.Sprintf("- [%s] %s", role, truncate(text, 220)))
		}

		if msg.Role == domain.RoleUser {
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
			continue
		}

		// Assistant-side style extraction (stylized digest).
		score := 0
		if containsAny(lower, "請", "必須", "應該", "務必", "must", "should") {
			directiveScore += 2
			styleMarkersSet["directive"] = true
			score += 2
		}
		if containsAny(lower, "愚", "蠢", "廢", "垃圾", "trash", "idiot", "stupid", "useless", "白痴", "笨") {
			criticalScore += 3
			irritatedScore += 2
			styleMarkersSet["abrasive"] = true
			score += 3
		}
		if containsAny(lower, "呵", "哈", "ma chérie", "呵呵", "哼") {
			playfulScore += 2
			styleMarkersSet["sarcastic"] = true
			score += 2
		}
		if containsAny(lower, "因此", "所以", "故", "therefore", "hence", "請注意", "綜上") {
			formalScore += 2
			composedScore += 2
			styleMarkersSet["formal-logic"] = true
			score += 2
		}
		if punctuationPattern.MatchString(text) {
			criticalScore++
			irritatedScore++
			styleMarkersSet["emphatic-punctuation"] = true
			score++
		}
		for _, term := range styleLexicon {
			if term == "" {
				continue
			}
			if strings.Contains(lower, strings.ToLower(term)) {
				signatureSet[term] = true
				score += 3
			}
		}
		if containsAny(lower, "冷靜", "計算", "理性", "analysis", "calculating", "logic") {
			composedScore += 2
			coldScore++
		}
		if containsAny(lower, "命令", "立刻", "現在", "馬上") {
			directiveScore++
			coldScore++
		}
		if containsAny(lower, "煩", "受不了", "氣死", "崩潰") {
			irritatedScore += 2
		}
		if containsAny(lower, "冷淡", "無聊", "毫無意義", "浪費時間") {
			coldScore += 2
		}

		if score > 0 {
			quotes = append(quotes, scoredQuote{
				text:  truncate(text, 220),
				score: score,
			})
		}
	}

	if len(summaryLines) == 0 {
		return domain.ConversationDigest{}, false
	}

	keywords := extractTopKeywords(strings.Join(allText, "\n"), 16)
	styleMarkers := make([]string, 0, len(styleMarkersSet))
	for marker := range styleMarkersSet {
		styleMarkers = append(styleMarkers, marker)
	}
	sort.Strings(styleMarkers)

	signatureTerms := make([]string, 0, len(signatureSet))
	for term := range signatureSet {
		signatureTerms = append(signatureTerms, term)
	}
	sort.Slice(signatureTerms, func(i, j int) bool {
		return len(signatureTerms[i]) > len(signatureTerms[j])
	})
	if len(signatureTerms) > 8 {
		signatureTerms = signatureTerms[:8]
	}

	sort.Slice(quotes, func(i, j int) bool {
		if quotes[i].score == quotes[j].score {
			return len(quotes[i].text) > len(quotes[j].text)
		}
		return quotes[i].score > quotes[j].score
	})
	styleQuotes := make([]string, 0, 3)
	for _, q := range quotes {
		styleQuotes = appendUnique(styleQuotes, q.text, 3)
		if len(styleQuotes) >= 3 {
			break
		}
	}

	attitude := classifyAttitude(criticalScore, directiveScore, playfulScore, formalScore)
	emotionalTone := classifyEmotionalTone(coldScore, irritatedScore, composedScore)
	if attitude != "" || emotionalTone != "" {
		prefix := "Style continuity:"
		if attitude != "" && emotionalTone != "" {
			prefix = fmt.Sprintf("Style continuity: %s / %s.", attitude, emotionalTone)
		} else if attitude != "" {
			prefix = fmt.Sprintf("Style continuity: %s.", attitude)
		} else if emotionalTone != "" {
			prefix = fmt.Sprintf("Style continuity: %s.", emotionalTone)
		}
		summaryLines = append([]string{prefix + " Preserve tone and persona-specific stance."}, summaryLines...)
	}

	digest := domain.ConversationDigest{
		ID:             fmt.Sprintf("digest-%d", time.Now().UnixNano()),
		Summary:        strings.Join(summaryLines, "\n"),
		Facts:          facts,
		Preferences:    preferences,
		OpenItems:      openItems,
		Decisions:      decisions,
		Keywords:       keywords,
		Attitude:       attitude,
		EmotionalTone:  emotionalTone,
		StyleMarkers:   styleMarkers,
		SignatureTerms: signatureTerms,
		StyleQuotes:    styleQuotes,
		CreatedAt:      time.Now(),
	}
	if attitude != "" || emotionalTone != "" || len(styleMarkers) > 0 || len(signatureTerms) > 0 || len(styleQuotes) > 0 {
		count := atomic.AddUint64(&digestStyleFieldCount, 1)
		logCtx.Logf("metric digest.style_fields_populated=%d", count)
	}
	if len(styleQuotes) > 0 {
		count := atomic.AddUint64(&digestStyleQuoteCount, uint64(len(styleQuotes)))
		logCtx.Logf("metric digest.style_quotes_count=%d", count)
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

func humanizeConversationTextForDigest(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	if _, ok := tooncodec.DecodeEventType(trimmed); ok {
		return strings.TrimSpace(tooncodec.HumanizeTOON(trimmed))
	}
	return trimmed
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

func classifyAttitude(critical, directive, playful, formal int) string {
	type candidate struct {
		label string
		score int
	}
	candidates := []candidate{
		{label: "critical", score: critical},
		{label: "directive", score: directive},
		{label: "playful", score: playful},
		{label: "formal", score: formal},
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) == 0 || candidates[0].score <= 0 {
		return ""
	}
	return candidates[0].label
}

func classifyEmotionalTone(cold, irritated, composed int) string {
	type candidate struct {
		label string
		score int
	}
	candidates := []candidate{
		{label: "cold", score: cold},
		{label: "irritated", score: irritated},
		{label: "composed", score: composed},
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) == 0 || candidates[0].score <= 0 {
		return ""
	}
	return candidates[0].label
}

func extractPersonaStyleLexicon(persona *domain.PersonaModule) []string {
	if persona == nil {
		return nil
	}
	set := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = true
		}
	}

	for _, t := range persona.Config.StyleGuard.SignatureTerms {
		add(t)
	}
	if raw, ok := persona.Config.Variables["insult_list"]; ok {
		switch v := raw.(type) {
		case []string:
			for _, item := range v {
				add(item)
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					add(s)
				}
			}
		}
	}

	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return len(out[i]) > len(out[j])
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func truncate(s string, maxLen int) string {
	return util.TruncateWithEllipsis(s, maxLen)
}
