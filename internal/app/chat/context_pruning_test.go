package chat

import (
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func makeToolResultContent(size int) string {
	encoded, err := tooncodec.EncodeToolResultsEvent([]tooncodec.ToolResultInput{
		{ID: "1", Name: "web_search", OK: true, Out: strings.Repeat("x", size)},
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

func buildTestMessages(userCount, assistantCount int, toolResultSize int) []domain.Message {
	var msgs []domain.Message
	for i := 0; i < userCount; i++ {
		msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: "user message " + strings.Repeat("a", 100)})
		if toolResultSize > 0 {
			msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: makeToolResultContent(toolResultSize)})
		}
		if i < assistantCount {
			msgs = append(msgs, domain.Message{Role: domain.RoleAssistant, Content: "assistant reply " + strings.Repeat("b", 100)})
		}
	}
	return msgs
}

func TestPruneToolResults_NoOpWhenBelowThreshold(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "hi there"},
	}
	settings := DefaultContextPruningSettings()
	// contextWindowChars is very large, so ratio < softTrimRatio
	result := pruneToolResults(msgs, settings, 1_000_000)

	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
	for i := range msgs {
		if result[i].Content != msgs[i].Content {
			t.Fatalf("message %d should be unchanged", i)
		}
	}
}

func TestPruneToolResults_SoftTrimLargeToolResults(t *testing.T) {
	toolContent := makeToolResultContent(6000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "first user message"},
		{Role: domain.RoleUser, Content: toolContent},
		{Role: domain.RoleAssistant, Content: "first reply"},
		{Role: domain.RoleUser, Content: "second message"},
		{Role: domain.RoleAssistant, Content: "second reply"},
		{Role: domain.RoleUser, Content: "third message"},
		{Role: domain.RoleAssistant, Content: "third reply"},
		{Role: domain.RoleUser, Content: "fourth message"},
		{Role: domain.RoleAssistant, Content: "fourth reply"},
	}

	settings := DefaultContextPruningSettings()
	settings.SoftTrimRatio = 0.1 // lower threshold to ensure we trigger
	totalChars := estimateMessagesChars(msgs)
	// Set contextWindowChars so ratio > softTrimRatio
	contextWindowChars := int(float64(totalChars) / 0.2)

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// The tool result at index 1 should be trimmed.
	if result[1].Content == toolContent {
		t.Fatalf("expected tool result to be soft-trimmed")
	}
	if !strings.Contains(result[1].Content, "Tool result trimmed") {
		t.Fatalf("expected trimming annotation, got: %s", result[1].Content[:200])
	}
	if len(result[1].Content) >= len(toolContent) {
		t.Fatalf("trimmed content should be shorter: trimmed=%d original=%d", len(result[1].Content), len(toolContent))
	}
	// Non-tool messages should be unchanged.
	if result[0].Content != msgs[0].Content {
		t.Fatalf("non-tool message should be unchanged")
	}
}

func TestPruneToolResults_HardClearWhenAboveRatio(t *testing.T) {
	// Create a conversation with large tool results.
	bigTool := makeToolResultContent(10000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "first"},
		{Role: domain.RoleUser, Content: bigTool},
		{Role: domain.RoleAssistant, Content: "reply 1"},
		{Role: domain.RoleUser, Content: "second"},
		{Role: domain.RoleUser, Content: bigTool},
		{Role: domain.RoleAssistant, Content: "reply 2"},
		{Role: domain.RoleUser, Content: "third"},
		{Role: domain.RoleUser, Content: bigTool},
		{Role: domain.RoleAssistant, Content: "reply 3"},
		{Role: domain.RoleUser, Content: "fourth"},
		{Role: domain.RoleUser, Content: bigTool},
		{Role: domain.RoleAssistant, Content: "reply 4"},
		{Role: domain.RoleUser, Content: "fifth"},
		{Role: domain.RoleUser, Content: bigTool},
		{Role: domain.RoleAssistant, Content: "reply 5"},
	}

	settings := DefaultContextPruningSettings()
	settings.SoftTrimRatio = 0.1
	settings.HardClearRatio = 0.2
	settings.MinPrunableToolChars = 100 // lower for test
	totalChars := estimateMessagesChars(msgs)
	contextWindowChars := int(float64(totalChars) / 0.5) // ratio = 0.5, well above hardClearRatio

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// At least some old tool results should be hard-cleared.
	hardCleared := 0
	for _, msg := range result {
		if msg.Content == settings.HardClearPlaceholder {
			hardCleared++
		}
	}
	if hardCleared == 0 {
		t.Fatalf("expected at least one tool result to be hard-cleared")
	}
}

func TestPruneToolResults_ProtectsRecentAssistantTurns(t *testing.T) {
	bigTool := makeToolResultContent(8000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "old user"},
		{Role: domain.RoleUser, Content: bigTool}, // old, prunable
		{Role: domain.RoleAssistant, Content: "old reply"},
		// Recent 3 assistant turns start here
		{Role: domain.RoleUser, Content: "recent 1"},
		{Role: domain.RoleUser, Content: bigTool}, // should be protected
		{Role: domain.RoleAssistant, Content: "recent reply 1"},
		{Role: domain.RoleUser, Content: "recent 2"},
		{Role: domain.RoleUser, Content: bigTool}, // should be protected
		{Role: domain.RoleAssistant, Content: "recent reply 2"},
		{Role: domain.RoleUser, Content: "recent 3"},
		{Role: domain.RoleUser, Content: bigTool}, // should be protected
		{Role: domain.RoleAssistant, Content: "recent reply 3"},
	}

	settings := DefaultContextPruningSettings()
	settings.KeepLastAssistantTurns = 3
	settings.SoftTrimRatio = 0.01 // always trigger
	totalChars := estimateMessagesChars(msgs)
	contextWindowChars := totalChars * 2

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// Tool results at indices 4, 7, 10 (in the protected tail) should be untouched.
	for _, idx := range []int{4, 7, 10} {
		if result[idx].Content != msgs[idx].Content {
			t.Fatalf("protected tool result at index %d was modified", idx)
		}
	}
	// Tool result at index 1 (before cutoff) should be trimmed.
	if result[1].Content == msgs[1].Content {
		t.Fatalf("old tool result at index 1 should have been trimmed")
	}
}

func TestPruneToolResults_ProtectsPreFirstUserMessages(t *testing.T) {
	bigTool := makeToolResultContent(8000)
	msgs := []domain.Message{
		{Role: domain.RoleAssistant, Content: bigTool}, // before first user — protected
		{Role: domain.RoleUser, Content: "first user msg"},
		{Role: domain.RoleUser, Content: bigTool}, // after first user — prunable
		{Role: domain.RoleAssistant, Content: "reply"},
	}

	settings := DefaultContextPruningSettings()
	settings.SoftTrimRatio = 0.01
	settings.KeepLastAssistantTurns = 0 // don't protect tail
	totalChars := estimateMessagesChars(msgs)
	contextWindowChars := totalChars * 2

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// First message (before first user) should be untouched.
	if result[0].Content != msgs[0].Content {
		t.Fatalf("pre-first-user message should be protected")
	}
	// Tool result at index 2 should be trimmed.
	if result[2].Content == msgs[2].Content {
		t.Fatalf("tool result after first user should be trimmed")
	}
}

func TestPruneToolResults_SkipsNonToolMessages(t *testing.T) {
	longContent := strings.Repeat("x", 8000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "first user"},
		{Role: domain.RoleUser, Content: longContent}, // long but NOT a tool result
		{Role: domain.RoleAssistant, Content: longContent},
	}

	settings := DefaultContextPruningSettings()
	settings.SoftTrimRatio = 0.01
	settings.KeepLastAssistantTurns = 0
	totalChars := estimateMessagesChars(msgs)
	contextWindowChars := totalChars * 2

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// No tool results to prune, everything should be unchanged.
	for i := range msgs {
		if result[i].Content != msgs[i].Content {
			t.Fatalf("non-tool message at index %d was modified", i)
		}
	}
}

func TestPruneToolResults_LegacyToolResultFormat(t *testing.T) {
	legacyTool := "Tool execution results:\n\nTool: web_search (ID: abc)\nStatus: SUCCESS\n" + strings.Repeat("x", 8000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "first user"},
		{Role: domain.RoleUser, Content: legacyTool},
		{Role: domain.RoleAssistant, Content: "reply"},
	}

	settings := DefaultContextPruningSettings()
	settings.SoftTrimRatio = 0.01
	settings.KeepLastAssistantTurns = 0
	totalChars := estimateMessagesChars(msgs)
	contextWindowChars := totalChars * 2

	result := pruneToolResults(msgs, settings, contextWindowChars)

	// Legacy tool result should be trimmed.
	if result[1].Content == msgs[1].Content {
		t.Fatalf("legacy tool result should have been trimmed")
	}
	if !strings.Contains(result[1].Content, "Tool result trimmed") {
		t.Fatalf("expected trimming annotation in legacy tool result")
	}
}

func TestPruneToolResults_EmptyMessages(t *testing.T) {
	result := pruneToolResults(nil, DefaultContextPruningSettings(), 100000)
	if result != nil {
		t.Fatalf("expected nil for empty input")
	}
}

func TestSoftTrimMessage_NoTrimWhenSmall(t *testing.T) {
	msg := domain.Message{
		Role:    domain.RoleUser,
		Content: strings.Repeat("x", 3000), // below 4000 threshold
	}
	settings := DefaultContextPruningSettings()
	result := softTrimMessage(msg, settings)
	if result != nil {
		t.Fatalf("expected nil when content is below maxChars")
	}
}

func TestSoftTrimMessage_TrimsLargeContent(t *testing.T) {
	content := strings.Repeat("x", 8000)
	msg := domain.Message{
		Role:    domain.RoleUser,
		Content: content,
	}
	settings := DefaultContextPruningSettings()
	result := softTrimMessage(msg, settings)
	if result == nil {
		t.Fatalf("expected trimmed result")
	}
	if len(result.Content) >= len(content) {
		t.Fatalf("trimmed content should be shorter")
	}
	// Should contain head and tail portions.
	if !strings.HasPrefix(result.Content, content[:100]) {
		t.Fatalf("trimmed content should start with original head")
	}
	if !strings.Contains(result.Content, content[len(content)-100:]) {
		t.Fatalf("trimmed content should contain original tail")
	}
}

func TestFindAssistantCutoffIndex(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "u1"},       // 0
		{Role: domain.RoleAssistant, Content: "a1"},   // 1
		{Role: domain.RoleUser, Content: "u2"},        // 2
		{Role: domain.RoleAssistant, Content: "a2"},   // 3
		{Role: domain.RoleUser, Content: "u3"},        // 4
		{Role: domain.RoleAssistant, Content: "a3"},   // 5
	}

	// keepLast=2 → 2nd-from-last assistant is a2 (index 3), walk back to user u2 (index 2)
	got := findAssistantCutoffIndex(msgs, 2)
	if got != 2 {
		t.Fatalf("findAssistantCutoffIndex(keep=2) = %d, want 2", got)
	}

	// keepLast=1 → last assistant is a3 (index 5), walk back to user u3 (index 4)
	got = findAssistantCutoffIndex(msgs, 1)
	if got != 4 {
		t.Fatalf("findAssistantCutoffIndex(keep=1) = %d, want 4", got)
	}

	// keepLast=0 → everything prunable
	got = findAssistantCutoffIndex(msgs, 0)
	if got != len(msgs) {
		t.Fatalf("findAssistantCutoffIndex(keep=0) = %d, want %d", got, len(msgs))
	}

	// keepLast > total assistants → protect everything
	got = findAssistantCutoffIndex(msgs, 10)
	if got != len(msgs) {
		t.Fatalf("findAssistantCutoffIndex(keep=10) = %d, want %d", got, len(msgs))
	}
}

func TestIsToolResultMessage(t *testing.T) {
	toolContent := makeToolResultContent(100)
	if !isToolResultMessage(toolContent) {
		t.Fatalf("expected TOON tool result to be recognized")
	}
	if isToolResultMessage("just a regular message") {
		t.Fatalf("regular message should not be recognized as tool result")
	}
	if isToolResultMessage("") {
		t.Fatalf("empty string should not be recognized as tool result")
	}
}
