package domain

import (
	"encoding/json"
	"testing"
)

func TestConversationJSONBackwardCompatibilityWithoutHistoryCutoff(t *testing.T) {
	raw := `{
		"id": "chan:persona",
		"channel_id": "chan",
		"persona_id": "persona",
		"messages": [],
		"created_at": "2026-02-14T00:00:00Z",
		"updated_at": "2026-02-14T00:00:00Z",
		"cumulative_tokens": 123
	}`

	var conv Conversation
	if err := json.Unmarshal([]byte(raw), &conv); err != nil {
		t.Fatalf("unmarshal legacy conversation: %v", err)
	}
	if conv.HistoryCutoffAt != nil {
		t.Fatalf("expected nil history_cutoff_at for legacy JSON, got %v", conv.HistoryCutoffAt)
	}
	if conv.CumulativeTokens != 123 {
		t.Fatalf("expected cumulative_tokens=123, got %d", conv.CumulativeTokens)
	}
}
