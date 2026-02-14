package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

var ErrConversationNotFound = errors.New("conversation not found")

// NewConversationID returns a unique conversation session ID.
func NewConversationID() string {
	return fmt.Sprintf("conv_%d", time.Now().UnixNano())
}

// ConversationRepository abstracts conversation persistence.
type ConversationRepository interface {
	// Get retrieves the active conversation by channel+persona pair.
	Get(ctx context.Context, channelID, personaID string) (*domain.Conversation, error)
	// GetByID retrieves a conversation by session ID.
	GetByID(ctx context.Context, conversationID string) (*domain.Conversation, error)
	// Save persists a conversation (create or update).
	Save(ctx context.Context, conv *domain.Conversation) error
	// Delete removes the active conversation for channel+persona.
	Delete(ctx context.Context, channelID, personaID string) error
	// DeleteByID removes a conversation by session ID.
	DeleteByID(ctx context.Context, conversationID string) error
	// CreateSession creates and activates a new empty session for channel+persona.
	CreateSession(ctx context.Context, channelID, personaID string, cutoff time.Time) (*domain.Conversation, error)
	// SetActive points channel+persona to an existing conversation session.
	SetActive(ctx context.Context, channelID, personaID, conversationID string) error
	// GetActiveID returns the active conversation session ID for channel+persona.
	GetActiveID(ctx context.Context, channelID, personaID string) (string, error)
	// List retrieves all conversations.
	List(ctx context.Context) ([]*domain.Conversation, error)
}
