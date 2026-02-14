package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// MemoryStore is an in-memory ConversationRepository for development.
type MemoryStore struct {
	mu      sync.RWMutex
	convs   map[string]*domain.Conversation // key: conversationID
	active  map[string]string               // key: "channelID:personaID" -> conversationID
	created map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		convs:   make(map[string]*domain.Conversation),
		active:  make(map[string]string),
		created: make(map[string]time.Time),
	}
}

func (m *MemoryStore) Get(_ context.Context, channelID, personaID string) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := compositeKey(channelID, personaID)
	if id, ok := m.active[key]; ok && id != "" {
		if conv, exists := m.convs[id]; exists {
			return conv, nil
		}
		delete(m.active, key)
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
	m.convs[conv.ID] = conv
	m.created[conv.ID] = conv.CreatedAt
	m.active[key] = conv.ID
	return conv, nil
}

func (m *MemoryStore) GetByID(_ context.Context, conversationID string) (*domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conv, exists := m.convs[conversationID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}
	return conv, nil
}

func (m *MemoryStore) Save(_ context.Context, conv *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv == nil {
		return fmt.Errorf("conversation is nil")
	}
	if conv.ChannelID == "" || conv.PersonaID == "" {
		return fmt.Errorf("channel_id and persona_id are required")
	}
	if conv.ID == "" {
		conv.ID = NewConversationID()
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = time.Now().UTC()
	}
	conv.UpdatedAt = time.Now().UTC()
	m.convs[conv.ID] = conv
	m.created[conv.ID] = conv.CreatedAt

	key := compositeKey(conv.ChannelID, conv.PersonaID)
	if current := m.active[key]; current == "" {
		m.active[key] = conv.ID
	}
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, channelID, personaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := compositeKey(channelID, personaID)
	if id := m.active[key]; id != "" {
		delete(m.convs, id)
		delete(m.created, id)
		delete(m.active, key)
	}
	return nil
}

func (m *MemoryStore) DeleteByID(_ context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, exists := m.convs[conversationID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}
	delete(m.convs, conversationID)
	delete(m.created, conversationID)

	key := compositeKey(conv.ChannelID, conv.PersonaID)
	if m.active[key] == conversationID {
		delete(m.active, key)
	}
	return nil
}

func (m *MemoryStore) CreateSession(_ context.Context, channelID, personaID string, cutoff time.Time) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

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

	m.convs[conv.ID] = conv
	m.created[conv.ID] = conv.CreatedAt
	m.active[compositeKey(channelID, personaID)] = conv.ID
	return conv, nil
}

func (m *MemoryStore) SetActive(_ context.Context, channelID, personaID, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, exists := m.convs[conversationID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}
	if conv.ChannelID != channelID || conv.PersonaID != personaID {
		return fmt.Errorf("conversation %s does not belong to %s/%s", conversationID, channelID, personaID)
	}

	m.active[compositeKey(channelID, personaID)] = conversationID
	return nil
}

func (m *MemoryStore) GetActiveID(_ context.Context, channelID, personaID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.active[compositeKey(channelID, personaID)], nil
}

func (m *MemoryStore) List(_ context.Context) ([]*domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conversations := make([]*domain.Conversation, 0, len(m.convs))
	for _, conv := range m.convs {
		conversations = append(conversations, conv)
	}

	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
	})

	return conversations, nil
}

func compositeKey(channelID, personaID string) string {
	return fmt.Sprintf("%s:%s", channelID, personaID)
}
