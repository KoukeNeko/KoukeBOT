package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// FileStore persists conversations as JSON files in a directory.
type FileStore struct {
	mu      sync.RWMutex
	dataDir string // e.g., "data/conversations"
}

// NewFileStore creates a FileStore that persists to the given directory.
// The directory will be created if it doesn't exist.
func NewFileStore(dataDir string) (*FileStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return &FileStore{dataDir: dataDir}, nil
}

func (f *FileStore) Get(_ context.Context, channelID, personaID string) (*domain.Conversation, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	path := f.filePath(channelID, personaID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Return a new empty conversation — not an error
		return &domain.Conversation{
			ID:        compositeKey(channelID, personaID),
			ChannelID: channelID,
			PersonaID: personaID,
			Messages:  make([]domain.Message, 0),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read conversation file: %w", err)
	}

	var conv domain.Conversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, fmt.Errorf("parse conversation JSON: %w", err)
	}

	return &conv, nil
}

func (f *FileStore) Save(_ context.Context, conv *domain.Conversation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	conv.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}

	path := f.filePath(conv.ChannelID, conv.PersonaID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write conversation file: %w", err)
	}

	return nil
}

func (f *FileStore) Delete(_ context.Context, channelID, personaID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := f.filePath(channelID, personaID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete conversation file: %w", err)
	}

	return nil
}

func (f *FileStore) List(ctx context.Context) ([]*domain.Conversation, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read conversations directory: %w", err)
	}

	var conversations []*domain.Conversation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(f.dataDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("WARNING: failed to read conversation file %s: %v", path, err)
			continue
		}

		var conv domain.Conversation
		if err := json.Unmarshal(data, &conv); err != nil {
			log.Printf("WARNING: failed to parse conversation file %s: %v", path, err)
			continue
		}

		conversations = append(conversations, &conv)
	}

	// Sort by most recent first
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
	})

	return conversations, nil
}

func (f *FileStore) filePath(channelID, personaID string) string {
	// Sanitize IDs to be safe for filenames
	safeChannel := sanitizeFilename(channelID)
	safePersona := sanitizeFilename(personaID)
	filename := fmt.Sprintf("%s_%s.json", safeChannel, safePersona)
	return filepath.Join(f.dataDir, filename)
}

// sanitizeFilename removes characters that are unsafe for filenames.
func sanitizeFilename(s string) string {
	// Simple approach: keep alphanumeric, dash, and underscore
	safe := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			safe += string(r)
		}
	}
	if safe == "" {
		return "unknown"
	}
	return safe
}
