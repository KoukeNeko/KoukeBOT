package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestFileStore_CreateSessionAndGetActive(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	cutoff := time.Date(2026, time.February, 15, 1, 2, 3, 0, time.UTC)
	created, err := repo.CreateSession(ctx, "ch-1", "satsuki", cutoff)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected conversation ID")
	}
	if created.HistoryCutoffAt == nil || !created.HistoryCutoffAt.Equal(cutoff) {
		t.Fatalf("expected cutoff=%s, got %v", cutoff, created.HistoryCutoffAt)
	}

	activeID, err := repo.GetActiveID(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != created.ID {
		t.Fatalf("active ID mismatch: got %s want %s", activeID, created.ID)
	}

	got, err := repo.Get(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("Get returned wrong conversation: got %s want %s", got.ID, created.ID)
	}
}

func TestFileStore_GetCreatesAndPersistsActiveSessionWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	conv, err := repo.Get(ctx, "ch-new", "persona-new")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conv.ID == "" {
		t.Fatal("expected generated conversation id")
	}

	activeID, err := repo.GetActiveID(ctx, "ch-new", "persona-new")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != conv.ID {
		t.Fatalf("active id = %s, want %s", activeID, conv.ID)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sessions", encodeConversationID(conv.ID)+".json")); err != nil {
		t.Fatalf("expected persisted session file, got err: %v", err)
	}
}

func TestFileStore_SaveDoesNotOverrideExistingActive(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	active, err := repo.CreateSession(ctx, "ch-1", "satsuki", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	archived := &domain.Conversation{
		ID:        "conv_archived",
		ChannelID: "ch-1",
		PersonaID: "satsuki",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "old"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Save(ctx, archived); err != nil {
		t.Fatalf("Save archived failed: %v", err)
	}

	activeID, err := repo.GetActiveID(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != active.ID {
		t.Fatalf("expected active to stay %s, got %s", active.ID, activeID)
	}
}

func TestFileStore_SetActive(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	first, err := repo.CreateSession(ctx, "ch-1", "satsuki", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateSession first failed: %v", err)
	}

	second := &domain.Conversation{
		ID:        "conv_second",
		ChannelID: "ch-1",
		PersonaID: "satsuki",
		Messages:  []domain.Message{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("Save second failed: %v", err)
	}

	if err := repo.SetActive(ctx, "ch-1", "satsuki", second.ID); err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}
	activeID, err := repo.GetActiveID(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != second.ID {
		t.Fatalf("expected active=%s got %s", second.ID, activeID)
	}

	got, err := repo.Get(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("expected Get to return second session %s, got %s", second.ID, got.ID)
	}
	if first.ID == got.ID {
		t.Fatalf("expected active switch away from first")
	}
}

func TestFileStore_DeleteByIDRemovesSessionAndActiveMapping(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	conv, err := repo.CreateSession(ctx, "ch-1", "satsuki", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := repo.DeleteByID(ctx, conv.ID); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	_, err = repo.GetByID(ctx, conv.ID)
	if !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}

	activeID, err := repo.GetActiveID(ctx, "ch-1", "satsuki")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != "" {
		t.Fatalf("expected active to be cleared, got %s", activeID)
	}
}

func TestFileStore_LegacyMigrationOnGet(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	channelID := "legacy-channel"
	personaID := "legacy-persona"
	legacyPath := filepath.Join(tmpDir, "legacy-channel_legacy-persona.json")
	legacyJSON := `{
  "id": "legacy-session-1",
  "channel_id": "legacy-channel",
  "persona_id": "legacy-persona",
  "messages": [{"role":"user","content":"hello","timestamp":"2026-02-14T00:00:00Z"}],
  "created_at": "2026-02-14T00:00:00Z",
  "updated_at": "2026-02-14T00:00:00Z"
}`
	if err := os.WriteFile(legacyPath, []byte(legacyJSON), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	ctx := context.Background()
	conv, err := repo.Get(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conv.ID != "legacy-session-1" {
		t.Fatalf("expected migrated ID=legacy-session-1, got %s", conv.ID)
	}

	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file should be removed after migration")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sessions", encodeConversationID(conv.ID)+".json")); err != nil {
		t.Fatalf("expected migrated session file, got err: %v", err)
	}

	activeID, err := repo.GetActiveID(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != conv.ID {
		t.Fatalf("expected active=%s got %s", conv.ID, activeID)
	}
}

func TestFileStore_SanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"with spaces", "withspaces"},
		{"with/slash", "withslash"},
		{"special!@#$%", "special"},
		{"", "unknown"},
	}

	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
