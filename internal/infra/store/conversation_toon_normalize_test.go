package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func TestFileStoreGetByIDLazyNormalizesLegacyConversationToTOON(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	convID := "conv-legacy-toon"
	legacy := domain.Conversation{
		ID:        convID,
		ChannelID: "legacy-ch",
		PersonaID: "persona-a",
		Messages: []domain.Message{
			{
				Role:      domain.RoleUser,
				Content:   "legacy user plain text",
				Timestamp: time.Date(2026, time.February, 15, 6, 0, 0, 0, time.UTC),
			},
			{
				Role:             domain.RoleAssistant,
				Content:          "legacy assistant plain text",
				RawLLMInput:      "=== SYSTEM PROMPT === very long prompt",
				RawLLMInputHuman: "human snapshot",
				Timestamp:        time.Date(2026, time.February, 15, 6, 1, 0, 0, time.UTC),
			},
		},
		CreatedAt: time.Date(2026, time.February, 15, 5, 59, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.February, 15, 6, 2, 0, 0, time.UTC),
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy conversation: %v", err)
	}
	path := filepath.Join(tmpDir, "sessions", encodeConversationID(convID)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write legacy conversation: %v", err)
	}

	got, err := repo.GetByID(context.Background(), convID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(got.Messages))
	}
	for i, msg := range got.Messages {
		eventType, ok := tooncodec.DecodeEventType(msg.Content)
		if !ok || eventType != tooncodec.EventTypeMessage {
			t.Fatalf("message[%d] should be TOON msg, got: %s", i, msg.Content)
		}
		if msg.RawLLMInput != "" || msg.RawLLMInputHuman != "" {
			t.Fatalf("message[%d] raw llm snapshots should be cleared", i)
		}
		if msg.DisplayText == "" {
			t.Fatalf("message[%d] display_text should be populated", i)
		}
	}
}

func TestMemoryStoreGetByIDLazyNormalizesLegacyConversationToTOON(t *testing.T) {
	repo := NewMemoryStore()
	convID := "conv-memory-legacy"
	repo.convs[convID] = &domain.Conversation{
		ID:        convID,
		ChannelID: "memory-ch",
		PersonaID: "persona-a",
		Messages: []domain.Message{
			{
				Role:      domain.RoleUser,
				Content:   "plain text from old memory store",
				Timestamp: time.Date(2026, time.February, 15, 7, 0, 0, 0, time.UTC),
			},
		},
		CreatedAt: time.Date(2026, time.February, 15, 7, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.February, 15, 7, 0, 0, 0, time.UTC),
	}

	got, err := repo.GetByID(context.Background(), convID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(got.Messages))
	}

	eventType, ok := tooncodec.DecodeEventType(got.Messages[0].Content)
	if !ok || eventType != tooncodec.EventTypeMessage {
		t.Fatalf("expected TOON msg content, got: %s", got.Messages[0].Content)
	}
	if got.Messages[0].DisplayText == "" {
		t.Fatalf("expected display_text to be populated")
	}
}

func TestNormalizeConversationMessagesToTOONIsIdempotent(t *testing.T) {
	now := time.Date(2026, time.February, 15, 8, 0, 0, 0, time.UTC)
	encoded, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    "api",
		UserID:    "u1",
		ChannelID: "ch",
		PersonaID: "persona-a",
		Timestamp: now,
		Text:      "already toon",
		Priority:  tooncodec.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("EncodeMessageEvent failed: %v", err)
	}

	conv := &domain.Conversation{
		ID:        "conv-idempotent",
		ChannelID: "ch",
		PersonaID: "persona-a",
		Messages: []domain.Message{
			{
				Role:             domain.RoleUser,
				Content:          encoded,
				DisplayText:      "already toon",
				RawLLMInput:      "old prompt snapshot",
				RawLLMInputHuman: "old prompt snapshot human",
				Timestamp:        now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	normalizeConversationMessagesToTOON(conv, now)
	first := conv.Messages[0].Content
	normalizeConversationMessagesToTOON(conv, now)
	second := conv.Messages[0].Content

	if first != second {
		t.Fatalf("expected idempotent normalized content, got first=%q second=%q", first, second)
	}
	if conv.Messages[0].RawLLMInput != "" || conv.Messages[0].RawLLMInputHuman != "" {
		t.Fatalf("expected raw input snapshots to be cleared")
	}
}
