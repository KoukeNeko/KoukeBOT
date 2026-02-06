package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// MemoryStore is an in-memory ConversationRepository for development.
type MemoryStore struct {
	mu    sync.RWMutex
	convs map[string]*domain.Conversation // key: "channelID:personaID"
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		convs: make(map[string]*domain.Conversation),
	}
}

func (m *MemoryStore) Get(_ context.Context, channelID, personaID string) (*domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := compositeKey(channelID, personaID)
	conv, exists := m.convs[key]
	if !exists {
		// Return a new empty conversation — not an error
		return &domain.Conversation{
			ID:        key,
			ChannelID: channelID,
			PersonaID: personaID,
			Messages:  make([]domain.Message, 0),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	return conv, nil
}

func (m *MemoryStore) Save(_ context.Context, conv *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := compositeKey(conv.ChannelID, conv.PersonaID)
	conv.UpdatedAt = time.Now()
	m.convs[key] = conv
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, channelID, personaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := compositeKey(channelID, personaID)
	delete(m.convs, key)
	return nil
}

func compositeKey(channelID, personaID string) string {
	return fmt.Sprintf("%s:%s", channelID, personaID)
}
