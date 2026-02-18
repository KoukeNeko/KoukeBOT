package chat

// assembler_history.go — recent message selection and related helpers.
//
// Responsibility: given a slice of conversation messages and a token budget,
// select the most recent user turns (and their associated assistant/tool
// responses) that fit within the budget.  This is intentionally separate from
// the main assembler so that the history-windowing logic can be read, tested,
// and reasoned about independently of prompt rendering.

import (
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

// selectRecentMessages selects the last maxUserTurns user turns and all their
// associated assistant/tool messages, subject to budgetTokens.
func (a *ContextAssembler) selectRecentMessages(messages []domain.Message, budgetTokens, maxUserTurns int) []domain.Message {
	if len(messages) == 0 {
		return nil
	}

	if maxUserTurns <= 0 {
		maxUserTurns = 20
	}

	// Step 1: Find the cutoff index by counting user turns backward.
	cutoff := findTurnCutoffByUserTurns(messages, maxUserTurns)
	candidate := messages[cutoff:]

	// Step 2: Filter out internal tool history and apply truncation + budget.
	const historyMessageTruncateChars = 4000
	selected := make([]domain.Message, 0, len(candidate))
	used := 0
	userTurnsSeen := 0
	for _, msg := range candidate {
		if isInternalToolHistoryMessage(msg.Content) {
			continue
		}
		compact := domain.Message{Role: msg.Role, Content: msg.Content}

		// Count user turns for truncation threshold (truncate older turns).
		if msg.Role == domain.RoleUser {
			userTurnsSeen++
		}
		// Truncate long messages beyond the 4th user turn from the end.
		turnsFromEnd := userTurnsSeen // approximate
		if turnsFromEnd > 4 && len(compact.Content) > historyMessageTruncateChars {
			compact.Content = compact.Content[:historyMessageTruncateChars] + "\n...[history truncated for token budget]"
		}

		cost := estimateTokens(compact.Content)
		if used+cost > budgetTokens {
			if len(selected) == 0 {
				// Always keep at least the latest message.
				selected = append(selected, compact)
			}
			break
		}

		used += cost
		selected = append(selected, compact)
	}

	return selected
}

// isInternalToolHistoryMessage reports whether a message is an internal tool
// execution marker that should be excluded from the assembled context.
// These messages exist for storage/debugging and are never shown to the LLM.
func isInternalToolHistoryMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if trimmed == "[Tool calls executed]" {
		return true
	}
	if eventType, ok := tooncodec.DecodeEventType(trimmed); ok && eventType == tooncodec.EventTypeToolResults {
		return true
	}
	return strings.HasPrefix(trimmed, "Tool execution results:")
}

// humanizeConversationContent converts TOON-encoded content to a readable form.
// Used when extracting the current message text for digest relevance scoring.
func humanizeConversationContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if _, ok := tooncodec.DecodeEventType(trimmed); ok {
		return strings.TrimSpace(tooncodec.HumanizeTOON(trimmed))
	}
	return trimmed
}
