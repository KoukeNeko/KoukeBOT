package chat

// assembler_digest.go — conversation digest selection and rendering.
//
// Responsibility: given the stored ConversationDigests and the current user
// message, select the most relevant digests and render them into a single
// prompt-ready string that fits within the digest token budget.
//
// Separation rationale: digest selection is keyword-scoring logic independent
// from both prompt rendering (assembler.go) and history windowing
// (assembler_history.go).  Keeping it here makes it easier to replace the
// scoring algorithm in isolation.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// selectRelevantDigests picks the top maxDigests digests most relevant to the
// current message, using keyword overlap + recency boost scoring.
// When the current message is empty (no keywords), only the latest digest is returned.
// The returned slice is in chronological order so the LLM sees context oldest-first.
func (a *ContextAssembler) selectRelevantDigests(digests []domain.ConversationDigest, currentMessage string, maxDigests int) []domain.ConversationDigest {
	if len(digests) == 0 {
		return nil
	}

	queryKeywords := extractTopKeywords(currentMessage, 20)
	keywordSet := make(map[string]bool, len(queryKeywords))
	for _, k := range queryKeywords {
		keywordSet[strings.ToLower(k)] = true
	}

	type scored struct {
		digest domain.ConversationDigest
		score  float64
	}
	items := make([]scored, 0, len(digests))
	for _, d := range digests {
		overlap := 0.0
		for _, k := range d.Keywords {
			if keywordSet[strings.ToLower(k)] {
				overlap += 1.0
			}
		}
		// Recency boost: later digests score slightly higher on equal keyword overlap.
		recencyBoost := float64(d.CreatedAt.Unix()) / 1e10
		score := overlap*5.0 + recencyBoost
		items = append(items, scored{digest: d, score: score})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].digest.CreatedAt.After(items[j].digest.CreatedAt)
		}
		return items[i].score > items[j].score
	})

	limit := maxDigests
	if limit <= 0 {
		limit = 3
	}
	if len(items) < limit {
		limit = len(items)
	}

	result := make([]domain.ConversationDigest, 0, limit)
	for i := 0; i < limit; i++ {
		// If query is empty, keep only the latest digest by default.
		if len(queryKeywords) == 0 && i > 0 {
			break
		}
		result = append(result, items[i].digest)
	}

	// Keep chronological order for readability in prompt.
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// buildDigestMessage renders selected digests into a single prompt string.
// The output fits within budgetTokens by hard-truncating the final string if needed.
func (a *ContextAssembler) buildDigestMessage(digests []domain.ConversationDigest, budgetTokens int) string {
	if len(digests) == 0 || budgetTokens <= 0 {
		return ""
	}

	maxChars := budgetTokens * estimatedCharsPerToken
	if maxChars < 400 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Relevant conversation digests]\n")
	for i, d := range digests {
		sb.WriteString(fmt.Sprintf("\nDigest %d (%s)\n", i+1, d.CreatedAt.Format(time.RFC3339)))
		if d.Attitude != "" || d.EmotionalTone != "" || len(d.StyleMarkers) > 0 || len(d.SignatureTerms) > 0 || len(d.StyleQuotes) > 0 {
			sb.WriteString("Style Contract:\n")
			if d.Attitude != "" {
				sb.WriteString("Attitude: " + d.Attitude + "\n")
			}
			if d.EmotionalTone != "" {
				sb.WriteString("Emotional tone: " + d.EmotionalTone + "\n")
			}
			if len(d.StyleMarkers) > 0 {
				sb.WriteString("Style markers: " + strings.Join(d.StyleMarkers, " | ") + "\n")
			}
			if len(d.SignatureTerms) > 0 {
				sb.WriteString("Signature terms: " + strings.Join(d.SignatureTerms, " | ") + "\n")
			}
			if len(d.StyleQuotes) > 0 {
				sb.WriteString("Style quotes:\n")
				for _, q := range d.StyleQuotes {
					sb.WriteString("- " + truncate(q, 180) + "\n")
				}
			}
			sb.WriteString("Directive: Do NOT flatten persona tone into a generic assistant voice.\n")
		}
		sb.WriteString("Summary:\n")
		sb.WriteString(truncate(d.Summary, 1200))
		sb.WriteString("\n")
		if len(d.Facts) > 0 {
			sb.WriteString("Facts: " + strings.Join(d.Facts, " | ") + "\n")
		}
		if len(d.Preferences) > 0 {
			sb.WriteString("Preferences: " + strings.Join(d.Preferences, " | ") + "\n")
		}
		if len(d.OpenItems) > 0 {
			sb.WriteString("Open items: " + strings.Join(d.OpenItems, " | ") + "\n")
		}
		if len(d.Decisions) > 0 {
			sb.WriteString("Decisions: " + strings.Join(d.Decisions, " | ") + "\n")
		}
	}

	msg := sb.String()
	if len(msg) > maxChars {
		msg = msg[:maxChars] + "\n...[digest truncated for token budget]"
	}
	return msg
}
