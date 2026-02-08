package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestFileStore_SaveAndGet(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	channelID := "test-channel"
	personaID := "test-persona"

	// Get non-existent conversation (should return empty)
	conv, err := store.Get(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conv.ChannelID != channelID || conv.PersonaID != personaID {
		t.Errorf("Got wrong conversation: %+v", conv)
	}
	if len(conv.Messages) != 0 {
		t.Errorf("Expected empty messages, got %d", len(conv.Messages))
	}

	// Add messages
	conv.AppendUserMessage("Hello")
	conv.AppendAssistantMessage("Hi there!")

	// Save
	if err := store.Save(ctx, conv); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	expectedPath := filepath.Join(tmpDir, "test-channel_test-persona.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("Expected file not found: %s", expectedPath)
	}

	// Get again (should load from file)
	conv2, err := store.Get(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("Get after save failed: %v", err)
	}
	if len(conv2.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(conv2.Messages))
	}
	if conv2.Messages[0].Role != "user" || conv2.Messages[0].Content != "Hello" {
		t.Errorf("Wrong first message: %+v", conv2.Messages[0])
	}
	if conv2.Messages[1].Role != "assistant" || conv2.Messages[1].Content != "Hi there!" {
		t.Errorf("Wrong second message: %+v", conv2.Messages[1])
	}
}

func TestFileStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	channelID := "test-channel"
	personaID := "test-persona"

	// Save a conversation
	conv := &domain.Conversation{
		ID:        "test-channel:test-persona",
		ChannelID: channelID,
		PersonaID: personaID,
		Messages: []domain.Message{
			{Role: "user", Content: "test", Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Save(ctx, conv); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Delete
	if err := store.Delete(ctx, channelID, personaID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file is gone
	expectedPath := filepath.Join(tmpDir, "test-channel_test-persona.json")
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Errorf("File should be deleted: %s", expectedPath)
	}

	// Get should return empty conversation
	conv2, err := store.Get(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if len(conv2.Messages) != 0 {
		t.Errorf("Expected empty conversation after delete, got %d messages", len(conv2.Messages))
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
