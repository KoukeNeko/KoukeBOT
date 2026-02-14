package discord

import (
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestResetConversationState(t *testing.T) {
	cutoff := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	conv := &domain.Conversation{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "hello"},
		},
		ContextDigests:   []domain.ConversationDigest{{ID: "d1", Summary: "old"}},
		CumulativeTokens: 42,
	}

	resetConversationState(conv, "ch-1", "satsuki", cutoff)

	if conv.ChannelID != "ch-1" || conv.PersonaID != "satsuki" {
		t.Fatalf("conversation identifiers not reset: channel=%s persona=%s", conv.ChannelID, conv.PersonaID)
	}
	if conv.ID != "ch-1:satsuki" {
		t.Fatalf("expected synthesized ID, got %q", conv.ID)
	}
	if len(conv.Messages) != 0 {
		t.Fatalf("expected empty messages, got %d", len(conv.Messages))
	}
	if conv.ContextDigests != nil {
		t.Fatalf("expected digests to be nil, got %v", conv.ContextDigests)
	}
	if conv.CumulativeTokens != 0 {
		t.Fatalf("expected cumulative tokens reset to 0, got %d", conv.CumulativeTokens)
	}
	if conv.HistoryCutoffAt == nil {
		t.Fatal("expected history cutoff to be set")
	}
	if !conv.HistoryCutoffAt.Equal(cutoff.UTC()) {
		t.Fatalf("expected cutoff %s, got %s", cutoff.UTC(), conv.HistoryCutoffAt.UTC())
	}
}

func TestResetConversationStatePreservesExistingIdentity(t *testing.T) {
	existingCreatedAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	conv := &domain.Conversation{
		ID:        "custom-id",
		CreatedAt: existingCreatedAt,
		Messages:  []domain.Message{{Role: domain.RoleAssistant, Content: "old"}},
	}
	cutoff := time.Date(2026, time.February, 14, 1, 2, 3, 0, time.UTC)

	resetConversationState(conv, "channel-x", "persona-y", cutoff)

	if conv.ID != "custom-id" {
		t.Fatalf("expected existing ID to be preserved, got %q", conv.ID)
	}
	if !conv.CreatedAt.Equal(existingCreatedAt) {
		t.Fatalf("expected created_at to stay %s, got %s", existingCreatedAt, conv.CreatedAt)
	}
	if conv.HistoryCutoffAt == nil || !conv.HistoryCutoffAt.Equal(cutoff) {
		t.Fatalf("expected cutoff %s, got %v", cutoff, conv.HistoryCutoffAt)
	}
}
