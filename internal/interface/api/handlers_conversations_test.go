package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestListConversationsIncludesResetConversation(t *testing.T) {
	convStore := store.NewMemoryStore()
	now := time.Date(2026, time.February, 14, 23, 30, 0, 0, time.UTC)
	cutoff := now.Add(-5 * time.Minute)

	if err := convStore.Save(context.Background(), &domain.Conversation{
		ID:              "ch-reset:persona-a",
		ChannelID:       "ch-reset",
		PersonaID:       "persona-a",
		Messages:        []domain.Message{},
		HistoryCutoffAt: &cutoff,
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("save reset conversation: %v", err)
	}

	if err := convStore.Save(context.Background(), &domain.Conversation{
		ID:        "ch-empty:persona-b",
		ChannelID: "ch-empty",
		PersonaID: "persona-b",
		Messages:  []domain.Message{},
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save empty conversation: %v", err)
	}

	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}

	req := httptest.NewRequest("GET", "/api/conversations", nil)
	rec := httptest.NewRecorder()
	h.ListConversations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got []ConversationSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	foundReset := false
	foundEmpty := false
	for _, item := range got {
		if item.ChannelID == "ch-reset" && item.PersonaID == "persona-a" {
			foundReset = true
			if item.MessageCount != 0 {
				t.Fatalf("reset conversation message_count = %d, want 0", item.MessageCount)
			}
			if item.LastMessage != "[對話已重置]" {
				t.Fatalf("reset conversation last_message = %q, want reset marker", item.LastMessage)
			}
		}
		if item.ChannelID == "ch-empty" && item.PersonaID == "persona-b" {
			foundEmpty = true
		}
	}

	if !foundReset {
		t.Fatalf("expected reset conversation to be listed")
	}
	if foundEmpty {
		t.Fatalf("expected plain empty conversation to be hidden")
	}
}
