package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logStore = logger.New("store", logger.Blue)

const (
	sessionsDirName = "sessions"
	activeIndexFile = "active_index.json"
)

// FileStore persists conversations as JSON files in a directory.
//
// Layout:
// - <dataDir>/sessions/<conversation-id-encoded>.json
// - <dataDir>/active_index.json  map["channel:persona"] = "conversationID"
//
// Legacy files (<dataDir>/<channel>_<persona>.json) are lazily migrated.
type FileStore struct {
	mu              sync.RWMutex
	dataDir         string
	sessionsDir     string
	activeIndexPath string
}

// NewFileStore creates a FileStore that persists to the given directory.
// The directory will be created if it doesn't exist.
func NewFileStore(dataDir string) (*FileStore, error) {
	sessionsDir := filepath.Join(dataDir, sessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create sessions directory: %w", err)
	}
	return &FileStore{
		dataDir:         dataDir,
		sessionsDir:     sessionsDir,
		activeIndexPath: filepath.Join(dataDir, activeIndexFile),
	}, nil
}

func (f *FileStore) Get(_ context.Context, channelID, personaID string) (*domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := compositeKey(channelID, personaID)
	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return nil, err
	}

	if conversationID := active[key]; conversationID != "" {
		conv, readErr := f.readConversationByIDLocked(conversationID)
		if readErr == nil {
			return conv, nil
		}
		if !errors.Is(readErr, ErrConversationNotFound) {
			return nil, readErr
		}
		// Active pointer is stale; clear it and continue fallback/migration path.
		delete(active, key)
		if err := f.saveActiveIndexLocked(active); err != nil {
			return nil, err
		}
	}

	migrated, err := f.migrateLegacyForPairLocked(channelID, personaID)
	if err != nil {
		return nil, err
	}
	if migrated != nil {
		return migrated, nil
	}

	now := time.Now().UTC()
	conv := &domain.Conversation{
		ID:        NewConversationID(),
		ChannelID: channelID,
		PersonaID: personaID,
		Messages:  make([]domain.Message, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := f.writeConversationLocked(conv); err != nil {
		return nil, err
	}
	active[key] = conv.ID
	if err := f.saveActiveIndexLocked(active); err != nil {
		return nil, err
	}
	return conv, nil
}

func (f *FileStore) GetByID(_ context.Context, conversationID string) (*domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	conv, err := f.readConversationByIDLocked(conversationID)
	if err == nil {
		return conv, nil
	}
	if !errors.Is(err, ErrConversationNotFound) {
		return nil, err
	}

	migrated, migrateErr := f.migrateLegacyByIDLocked(conversationID)
	if migrateErr == nil {
		return migrated, nil
	}
	if errors.Is(migrateErr, ErrConversationNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}
	return nil, migrateErr
}

func (f *FileStore) Save(_ context.Context, conv *domain.Conversation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if conv == nil {
		return fmt.Errorf("conversation is nil")
	}
	if strings.TrimSpace(conv.ChannelID) == "" || strings.TrimSpace(conv.PersonaID) == "" {
		return fmt.Errorf("channel_id and persona_id are required")
	}

	now := time.Now().UTC()
	normalizeConversation(conv, now)
	if err := f.writeConversationLocked(conv); err != nil {
		return err
	}

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return err
	}
	key := compositeKey(conv.ChannelID, conv.PersonaID)
	if active[key] == "" {
		active[key] = conv.ID
		if err := f.saveActiveIndexLocked(active); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileStore) Delete(_ context.Context, channelID, personaID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return err
	}

	key := compositeKey(channelID, personaID)
	if conversationID := active[key]; conversationID != "" {
		if err := os.Remove(f.sessionPath(conversationID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete conversation session: %w", err)
		}
		delete(active, key)
		if err := f.saveActiveIndexLocked(active); err != nil {
			return err
		}
	}

	legacyPath := f.legacyPath(channelID, personaID)
	if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete legacy conversation file: %w", err)
	}
	return nil
}

func (f *FileStore) DeleteByID(_ context.Context, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	conv, err := f.readConversationByIDLocked(conversationID)
	if err != nil {
		if !errors.Is(err, ErrConversationNotFound) {
			return err
		}

		legacyPath, legacyConv, legacyErr := f.findLegacyByConversationIDLocked(conversationID)
		if legacyErr != nil {
			return legacyErr
		}
		if legacyPath == "" || legacyConv == nil {
			return fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
		}
		conv = legacyConv
		if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete legacy conversation file: %w", err)
		}
	} else {
		if err := os.Remove(f.sessionPath(conversationID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete conversation session: %w", err)
		}
	}

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return err
	}

	changed := false
	for key, id := range active {
		if id == conversationID {
			delete(active, key)
			changed = true
		}
	}
	if conv != nil {
		key := compositeKey(conv.ChannelID, conv.PersonaID)
		if active[key] == conversationID {
			delete(active, key)
			changed = true
		}
	}
	if changed {
		if err := f.saveActiveIndexLocked(active); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileStore) CreateSession(_ context.Context, channelID, personaID string, cutoff time.Time) (*domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now().UTC()
	conv := &domain.Conversation{
		ID:        NewConversationID(),
		ChannelID: channelID,
		PersonaID: personaID,
		Messages:  make([]domain.Message, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if !cutoff.IsZero() {
		cutoffUTC := cutoff.UTC()
		conv.HistoryCutoffAt = &cutoffUTC
	}

	if err := f.writeConversationLocked(conv); err != nil {
		return nil, err
	}

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return nil, err
	}
	active[compositeKey(channelID, personaID)] = conv.ID
	if err := f.saveActiveIndexLocked(active); err != nil {
		return nil, err
	}
	return conv, nil
}

func (f *FileStore) SetActive(_ context.Context, channelID, personaID, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	conv, err := f.readConversationByIDLocked(conversationID)
	if err != nil {
		return err
	}
	if conv.ChannelID != channelID || conv.PersonaID != personaID {
		return fmt.Errorf("conversation %s does not belong to %s/%s", conversationID, channelID, personaID)
	}

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return err
	}
	active[compositeKey(channelID, personaID)] = conversationID
	return f.saveActiveIndexLocked(active)
}

func (f *FileStore) GetActiveID(_ context.Context, channelID, personaID string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return "", err
	}
	return active[compositeKey(channelID, personaID)], nil
}

func (f *FileStore) List(_ context.Context) ([]*domain.Conversation, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var conversations []*domain.Conversation
	seen := make(map[string]bool)

	sessionEntries, err := os.ReadDir(f.sessionsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}
	for _, entry := range sessionEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(f.sessionsDir, entry.Name())
		conv, err := f.readConversationFile(path)
		if err != nil {
			logStore.Warnf("failed to parse conversation file %s: %v", path, err)
			continue
		}
		if conv.ID == "" {
			continue
		}
		if seen[conv.ID] {
			continue
		}
		seen[conv.ID] = true
		conversations = append(conversations, conv)
	}

	// Keep compatibility with older root-level conversation files.
	rootEntries, err := os.ReadDir(f.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read conversations directory: %w", err)
	}
	for _, entry := range rootEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == activeIndexFile {
			continue
		}
		path := filepath.Join(f.dataDir, entry.Name())
		conv, err := f.readConversationFile(path)
		if err != nil {
			logStore.Warnf("failed to parse legacy conversation file %s: %v", path, err)
			continue
		}
		if conv.ID == "" {
			conv.ID = compositeKey(conv.ChannelID, conv.PersonaID)
		}
		if seen[conv.ID] {
			continue
		}
		seen[conv.ID] = true
		conversations = append(conversations, conv)
	}

	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
	})
	return conversations, nil
}

func (f *FileStore) readConversationByIDLocked(conversationID string) (*domain.Conversation, error) {
	path := f.sessionPath(conversationID)
	conv, err := f.readConversationFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
		}
		return nil, err
	}
	if conv.ID == "" {
		conv.ID = conversationID
	}
	return conv, nil
}

func (f *FileStore) writeConversationLocked(conv *domain.Conversation) error {
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	if err := os.WriteFile(f.sessionPath(conv.ID), data, 0o644); err != nil {
		return fmt.Errorf("write conversation file: %w", err)
	}
	return nil
}

func (f *FileStore) readConversationFile(path string) (*domain.Conversation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var conv domain.Conversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, fmt.Errorf("parse conversation JSON: %w", err)
	}
	if conv.Messages == nil {
		conv.Messages = make([]domain.Message, 0)
	}
	return &conv, nil
}

func (f *FileStore) migrateLegacyForPairLocked(channelID, personaID string) (*domain.Conversation, error) {
	path := f.legacyPath(channelID, personaID)
	conv, err := f.readConversationFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read legacy conversation: %w", err)
	}

	now := time.Now().UTC()
	if conv.ID == "" || conv.ID == compositeKey(channelID, personaID) {
		conv.ID = NewConversationID()
	}
	if conv.ChannelID == "" {
		conv.ChannelID = channelID
	}
	if conv.PersonaID == "" {
		conv.PersonaID = personaID
	}
	normalizeConversation(conv, now)

	if err := f.writeConversationLocked(conv); err != nil {
		return nil, err
	}

	active, err := f.loadActiveIndexLocked()
	if err != nil {
		return nil, err
	}
	active[compositeKey(channelID, personaID)] = conv.ID
	if err := f.saveActiveIndexLocked(active); err != nil {
		return nil, err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove legacy conversation file: %w", err)
	}
	logStore.Logf("migrated legacy conversation to session: %s/%s -> %s", channelID, personaID, conv.ID)
	return conv, nil
}

func (f *FileStore) migrateLegacyByIDLocked(conversationID string) (*domain.Conversation, error) {
	path, conv, err := f.findLegacyByConversationIDLocked(conversationID)
	if err != nil {
		return nil, err
	}
	if path == "" || conv == nil {
		return nil, fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}

	now := time.Now().UTC()
	if conv.ID == "" {
		conv.ID = conversationID
	}
	normalizeConversation(conv, now)

	if err := f.writeConversationLocked(conv); err != nil {
		return nil, err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove legacy conversation file: %w", err)
	}

	if conv.ChannelID != "" && conv.PersonaID != "" {
		active, err := f.loadActiveIndexLocked()
		if err != nil {
			return nil, err
		}
		key := compositeKey(conv.ChannelID, conv.PersonaID)
		if active[key] == "" {
			active[key] = conv.ID
			if err := f.saveActiveIndexLocked(active); err != nil {
				return nil, err
			}
		}
	}

	logStore.Logf("migrated legacy conversation by ID: %s", conv.ID)
	return conv, nil
}

func (f *FileStore) findLegacyByConversationIDLocked(conversationID string) (string, *domain.Conversation, error) {
	entries, err := os.ReadDir(f.dataDir)
	if err != nil {
		return "", nil, fmt.Errorf("read conversations directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == activeIndexFile {
			continue
		}
		path := filepath.Join(f.dataDir, entry.Name())
		conv, err := f.readConversationFile(path)
		if err != nil {
			logStore.Warnf("failed to read legacy conversation %s: %v", path, err)
			continue
		}
		if conv.ID == "" && conv.ChannelID != "" && conv.PersonaID != "" {
			conv.ID = compositeKey(conv.ChannelID, conv.PersonaID)
		}
		if conv.ID == conversationID {
			return path, conv, nil
		}
	}
	return "", nil, nil
}

func (f *FileStore) loadActiveIndexLocked() (map[string]string, error) {
	data, err := os.ReadFile(f.activeIndexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read active index: %w", err)
	}
	index := make(map[string]string)
	if len(data) == 0 {
		return index, nil
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse active index: %w", err)
	}
	return index, nil
}

func (f *FileStore) saveActiveIndexLocked(index map[string]string) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active index: %w", err)
	}
	if err := os.WriteFile(f.activeIndexPath, data, 0o644); err != nil {
		return fmt.Errorf("write active index: %w", err)
	}
	return nil
}

func (f *FileStore) sessionPath(conversationID string) string {
	return filepath.Join(f.sessionsDir, encodeConversationID(conversationID)+".json")
}

// legacyPath is kept for backward-compat migration from previous storage layout.
func (f *FileStore) legacyPath(channelID, personaID string) string {
	safeChannel := sanitizeFilename(channelID)
	safePersona := sanitizeFilename(personaID)
	filename := fmt.Sprintf("%s_%s.json", safeChannel, safePersona)
	return filepath.Join(f.dataDir, filename)
}

func encodeConversationID(conversationID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(conversationID))
}

func normalizeConversation(conv *domain.Conversation, now time.Time) {
	if conv.ID == "" {
		conv.ID = NewConversationID()
	}
	if conv.Messages == nil {
		conv.Messages = make([]domain.Message, 0)
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	conv.UpdatedAt = now
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
