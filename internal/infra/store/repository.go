package store

import (
	"context"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// ConversationRepository abstracts conversation persistence.
type ConversationRepository interface {
	// Get retrieves a conversation by channel+persona pair.
	Get(ctx context.Context, channelID, personaID string) (*domain.Conversation, error)
	// Save persists a conversation (create or update).
	Save(ctx context.Context, conv *domain.Conversation) error
	// Delete removes a conversation.
	Delete(ctx context.Context, channelID, personaID string) error
}
