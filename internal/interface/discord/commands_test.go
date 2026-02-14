package discord

import (
	"context"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestRotateResetSessionCreatesNewActiveAndPreservesOld(t *testing.T) {
	ctx := context.Background()
	convStore := store.NewMemoryStore()

	oldCutoff := time.Date(2026, time.February, 14, 8, 0, 0, 0, time.UTC)
	oldSession, err := convStore.CreateSession(ctx, "ch-1", "satsuki", oldCutoff)
	if err != nil {
		t.Fatalf("CreateSession old failed: %v", err)
	}
	oldSession.Messages = append(oldSession.Messages, domain.Message{
		Role:      domain.RoleUser,
		Content:   "hello",
		Timestamp: time.Now().UTC(),
	})
	if err := convStore.Save(ctx, oldSession); err != nil {
		t.Fatalf("Save old session failed: %v", err)
	}

	newCutoff := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	previous, current, err := rotateResetSession(ctx, convStore, "ch-1", "satsuki", newCutoff)
	if err != nil {
		t.Fatalf("rotateResetSession failed: %v", err)
	}

	if previous == nil || previous.ID != oldSession.ID {
		t.Fatalf("expected previous session %s, got %+v", oldSession.ID, previous)
	}
	if current == nil {
		t.Fatal("expected new active session")
	}
	if current.ID == oldSession.ID {
		t.Fatalf("expected new session ID, got same ID %s", current.ID)
	}
	if len(current.Messages) != 0 {
		t.Fatalf("expected new session to be empty, got %d messages", len(current.Messages))
	}
	if current.HistoryCutoffAt == nil || !current.HistoryCutoffAt.Equal(newCutoff.UTC()) {
		t.Fatalf("expected cutoff %s, got %v", newCutoff.UTC(), current.HistoryCutoffAt)
	}

	activeID, err := convStore.GetActiveID(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != current.ID {
		t.Fatalf("expected active ID %s, got %s", current.ID, activeID)
	}

	preserved, err := convStore.GetByID(ctx, oldSession.ID)
	if err != nil {
		t.Fatalf("GetByID old session failed: %v", err)
	}
	if len(preserved.Messages) != 1 || preserved.Messages[0].Content != "hello" {
		t.Fatalf("expected old session messages preserved, got %+v", preserved.Messages)
	}
}
