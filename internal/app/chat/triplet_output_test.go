package chat

import (
	"strings"
	"testing"
)

func TestPrepareAssistantOutputParsesMentionPrefixedTriplet(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `<@123456789> [Status]: Irritated
[Analysis]: Critical logic issue detected.
[Response]:
已完成修正。`

	view := prepareAssistantOutput(raw, policy)
	if !view.Parsed {
		t.Fatalf("expected parsed output, reason=%s", view.Reason)
	}
	if view.Content != "已完成修正。" {
		t.Fatalf("content = %q, want %q", view.Content, "已完成修正。")
	}
}

func TestPrepareAssistantOutputParsesBlockquoteTriplet(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `> [Status]: Advisory
> [Analysis]: Query normalized.
> [Response]:
> 好，已處理。`

	view := prepareAssistantOutput(raw, policy)
	if !view.Parsed {
		t.Fatalf("expected parsed output, reason=%s", view.Reason)
	}
	if view.Content != "好，已處理。" {
		t.Fatalf("content = %q, want %q", view.Content, "好，已處理。")
	}
}

func TestPrepareAssistantOutputUsesLastValidTriplet(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `[Status]: Old
[Analysis]: First pass.
[Response]: 舊回覆

[Status]: New
[Analysis]: Second pass.
[Response]: 新回覆`

	view := prepareAssistantOutput(raw, policy)
	if !view.Parsed {
		t.Fatalf("expected parsed output, reason=%s", view.Reason)
	}
	if view.Content != "新回覆" {
		t.Fatalf("content = %q, want %q", view.Content, "新回覆")
	}
	if view.Sections.Status != "New" {
		t.Fatalf("status = %q, want %q", view.Sections.Status, "New")
	}
}

func TestPrepareAssistantOutputFailsOnEmptyResponse(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `[Status]: Calm
[Analysis]: Waiting.
[Response]:   `

	view := prepareAssistantOutput(raw, policy)
	if view.Parsed {
		t.Fatalf("expected parse failure")
	}
	if view.Reason != tripletParseReasonEmptyResponse {
		t.Fatalf("reason = %q, want %q", view.Reason, tripletParseReasonEmptyResponse)
	}
	if view.Content != strings.TrimSpace(raw) {
		t.Fatalf("content = %q, want raw", view.Content)
	}
}

func TestPrepareAssistantOutputSalvagesResponseOnlyOutput(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	raw := `prefix noise
[Response]: 只保留這段回覆`

	view := prepareAssistantOutput(raw, policy)
	if view.Parsed {
		t.Fatalf("expected non-parsed salvage output")
	}
	if !view.Salvaged {
		t.Fatalf("expected salvaged output")
	}
	if view.Content != "只保留這段回覆" {
		t.Fatalf("content = %q, want %q", view.Content, "只保留這段回覆")
	}
	if view.Reason != tripletParseReasonOrderInvalidAndSalvage {
		t.Fatalf("reason = %q, want %q", view.Reason, tripletParseReasonOrderInvalidAndSalvage)
	}
}

func TestPrepareAssistantOutputHiddenModeUsesSalvagedResponseDisplay(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	policy.AnalysisVisibility = AnalysisVisibilityHidden
	raw := `debug block
[Response]: 顯示這句`

	view := prepareAssistantOutput(raw, policy)
	if view.Display != "顯示這句" {
		t.Fatalf("display = %q, want %q", view.Display, "顯示這句")
	}
}
