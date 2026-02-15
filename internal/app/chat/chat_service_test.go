package chat

import (
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func TestCompressToolResultKeepsWebFetchMetadata(t *testing.T) {
	content := "URL: https://example.com\nTitle: Example\n--- CONTENT START ---\n" +
		strings.Repeat("A", 9000)

	compressed := compressToolResult("web_fetch", content)

	if !strings.Contains(compressed, "URL: https://example.com") {
		t.Fatalf("compressed result must keep source URL metadata")
	}
	if len(compressed) >= len(content) {
		t.Fatalf("compressed content should be shorter than original")
	}
	if !strings.Contains(compressed, "truncated for token efficiency") {
		t.Fatalf("compressed output should include truncation marker")
	}
}

func TestPrepareAssistantOutputParsesTripletFormat(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `[Status]: Calm
[Analysis]: User asks for a straightforward weather answer.
[Response]:
今天會下雨，出門記得帶傘。`

	view := prepareAssistantOutput(raw, policy)
	if !view.Parsed {
		t.Fatalf("expected triplet output to parse")
	}
	if view.Content != "今天會下雨，出門記得帶傘。" {
		t.Fatalf("parsed response = %q", view.Content)
	}
	if !strings.Contains(view.Display, "[Analysis]") {
		t.Fatalf("visible mode should keep analysis in display")
	}
}

func TestPrepareAssistantOutputFallsBackWhenNoResponseSection(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := "好的，我明白了。"

	view := prepareAssistantOutput(raw, policy)
	if view.Parsed {
		t.Fatalf("expected parse fallback for non-triplet output")
	}
	if view.Content != raw {
		t.Fatalf("fallback content = %q, want %q", view.Content, raw)
	}
}

func TestFormatToolResultsEncodesTOONToolResultsEvent(t *testing.T) {
	calls := []domain.ToolCall{
		{ID: "a1", Name: "discord_observe"},
	}
	results := []domain.ToolResult{
		{ToolCallID: "a1", Content: `{"ok":true}`, IsError: false},
	}

	encoded := formatToolResults(calls, results)
	eventType, ok := tooncodec.DecodeEventType(encoded)
	if !ok {
		t.Fatalf("expected TOON payload, got: %s", encoded)
	}
	if eventType != tooncodec.EventTypeToolResults {
		t.Fatalf("event type = %s, want %s", eventType, tooncodec.EventTypeToolResults)
	}
}
