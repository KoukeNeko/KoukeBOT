package chat

import (
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

// ContextPruningSettings controls two-phase tool result pruning behavior.
// Inspired by OpenClaw's context-pruning extension.
type ContextPruningSettings struct {
	SoftTrimRatio          float64 // context usage ratio to trigger softTrim (default 0.3)
	HardClearRatio         float64 // context usage ratio to trigger hardClear (default 0.5)
	KeepLastAssistantTurns int     // protect tool results from the last N assistant turns (default 3)
	SoftTrimMaxChars       int     // tool result exceeding this triggers softTrim (default 4000)
	SoftTrimHeadChars      int     // chars to keep from head during softTrim (default 1500)
	SoftTrimTailChars      int     // chars to keep from tail during softTrim (default 1500)
	HardClearEnabled       bool    // whether hardClear phase is enabled (default true)
	HardClearPlaceholder   string  // replacement text for hardClear
	MinPrunableToolChars   int     // skip hardClear if prunable tool chars below this (default 50000)
}

const defaultHardClearPlaceholder = "[Tool result cleared to free context space]"

func DefaultContextPruningSettings() ContextPruningSettings {
	return ContextPruningSettings{
		SoftTrimRatio:          0.3,
		HardClearRatio:         0.5,
		KeepLastAssistantTurns: 3,
		SoftTrimMaxChars:       4000,
		SoftTrimHeadChars:      1500,
		SoftTrimTailChars:      1500,
		HardClearEnabled:       true,
		HardClearPlaceholder:   defaultHardClearPlaceholder,
		MinPrunableToolChars:   50_000,
	}
}

// pruneToolResults applies two-phase pruning (softTrim + hardClear) to old tool
// result messages. It returns a new slice; the original messages are not mutated.
// contextWindowChars is the available character budget (contextWindow tokens * 4).
func pruneToolResults(messages []domain.Message, settings ContextPruningSettings, contextWindowChars int) []domain.Message {
	if len(messages) == 0 || contextWindowChars <= 0 {
		return messages
	}

	totalChars := estimateMessagesChars(messages)
	ratio := float64(totalChars) / float64(contextWindowChars)
	if ratio < settings.SoftTrimRatio {
		return messages
	}

	cutoffIdx := findAssistantCutoffIndex(messages, settings.KeepLastAssistantTurns)
	firstUserIdx := findFirstUserIndex(messages)

	// Collect indices of prunable tool result messages.
	prunableIndices := collectPrunableToolIndices(messages, firstUserIdx, cutoffIdx)
	if len(prunableIndices) == 0 {
		return messages
	}

	// Phase 1: softTrim — truncate large tool results to head+tail.
	var result []domain.Message
	for _, idx := range prunableIndices {
		trimmed := softTrimMessage(messages[idx], settings)
		if trimmed == nil {
			continue
		}
		if result == nil {
			result = copyMessages(messages)
		}
		beforeChars := len(messages[idx].Content)
		afterChars := len(trimmed.Content)
		totalChars += afterChars - beforeChars
		result[idx] = *trimmed
	}

	output := result
	if output == nil {
		output = messages
	}
	ratio = float64(totalChars) / float64(contextWindowChars)
	if ratio < settings.HardClearRatio || !settings.HardClearEnabled {
		return output
	}

	// Check minimum prunable tool chars threshold before hardClear.
	prunableToolChars := 0
	for _, idx := range prunableIndices {
		prunableToolChars += len(output[idx].Content)
	}
	if prunableToolChars < settings.MinPrunableToolChars {
		return output
	}

	// Phase 2: hardClear — replace oldest tool results with placeholder.
	for _, idx := range prunableIndices {
		if ratio < settings.HardClearRatio {
			break
		}
		msg := output[idx]
		if result == nil {
			result = copyMessages(messages)
			output = result
		}
		beforeChars := len(msg.Content)
		result[idx] = domain.Message{
			Role:    msg.Role,
			Content: settings.HardClearPlaceholder,
		}
		afterChars := len(settings.HardClearPlaceholder)
		totalChars += afterChars - beforeChars
		ratio = float64(totalChars) / float64(contextWindowChars)
	}

	if result != nil {
		return result
	}
	return output
}

// estimateMessagesChars sums up character lengths of all message contents.
func estimateMessagesChars(messages []domain.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
	}
	return total
}

// findAssistantCutoffIndex returns the index where the protected tail begins.
// It finds the Nth-from-last assistant turn and walks back to the user message
// that started that turn, protecting the entire turn (user + tool results + assistant).
// Returns len(messages) if keepLast <= 0 (everything is prunable).
// Returns len(messages) if not enough assistant messages exist.
func findAssistantCutoffIndex(messages []domain.Message, keepLast int) int {
	if keepLast <= 0 {
		return len(messages)
	}
	remaining := keepLast
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != domain.RoleAssistant {
			continue
		}
		remaining--
		if remaining == 0 {
			// Walk backward to find the user message that started this turn.
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == domain.RoleUser {
					return j
				}
			}
			return i
		}
	}
	// Not enough assistant messages — protect everything.
	return len(messages)
}

// findFirstUserIndex returns the index of the first user message.
// Messages before this index are protected (system identity, SOUL.md, etc.).
func findFirstUserIndex(messages []domain.Message) int {
	for i, msg := range messages {
		if msg.Role == domain.RoleUser {
			return i
		}
	}
	return len(messages)
}

// isToolResultMessage checks if a message is a TOON-encoded tool result.
func isToolResultMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	eventType, ok := tooncodec.DecodeEventType(trimmed)
	if !ok {
		return false
	}
	return eventType == tooncodec.EventTypeToolResults
}

// collectPrunableToolIndices returns sorted indices of tool result messages
// within the prunable range [firstUserIdx, cutoffIdx).
func collectPrunableToolIndices(messages []domain.Message, firstUserIdx, cutoffIdx int) []int {
	var indices []int
	for i := firstUserIdx; i < cutoffIdx && i < len(messages); i++ {
		msg := messages[i]
		// Skip messages with image refs — they may be directly relevant.
		if len(msg.ImageRefs) > 0 {
			continue
		}
		if isToolResultMessage(msg.Content) {
			indices = append(indices, i)
		}
		// Also match legacy tool result format.
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "Tool execution results:") {
			indices = append(indices, i)
		}
	}
	return indices
}

// softTrimMessage trims a tool result message to head+tail if it exceeds maxChars.
// Returns nil if no trimming was needed.
func softTrimMessage(msg domain.Message, settings ContextPruningSettings) *domain.Message {
	content := msg.Content
	if len(content) <= settings.SoftTrimMaxChars {
		return nil
	}

	headChars := settings.SoftTrimHeadChars
	tailChars := settings.SoftTrimTailChars
	if headChars+tailChars >= len(content) {
		return nil
	}

	head := content[:headChars]
	tail := content[len(content)-tailChars:]
	note := fmt.Sprintf("\n[Tool result trimmed: kept first %d and last %d of %d chars]", headChars, tailChars, len(content))
	trimmed := head + "\n...\n" + tail + note

	return &domain.Message{
		Role:    msg.Role,
		Content: trimmed,
	}
}

func copyMessages(messages []domain.Message) []domain.Message {
	result := make([]domain.Message, len(messages))
	copy(result, messages)
	return result
}
