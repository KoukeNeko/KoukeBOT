package chat

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func (s *ChatService) resolveConversationAndPersona(ctx context.Context, channelID, personaID, conversationID string) (*domain.Conversation, *domain.PersonaModule, string, error) {
	if conversationID != "" {
		conv, err := s.store.GetByID(ctx, conversationID)
		if err != nil {
			return nil, nil, "", fmt.Errorf("get conversation by id: %w", err)
		}
		if conv == nil {
			return nil, nil, "", fmt.Errorf("conversation %q not found", conversationID)
		}
		persona, err := s.resolvePersona(conv.PersonaID)
		if err != nil {
			return nil, nil, "", err
		}
		return conv, persona, conv.PersonaID, nil
	}

	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, nil, "", err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get conversation: %w", err)
	}
	return conv, persona, personaID, nil
}

func (s *ChatService) resolvePersona(personaID string) (*domain.PersonaModule, error) {
	persona, exists := s.personas[personaID]
	if !exists {
		return nil, fmt.Errorf("persona %q not found", personaID)
	}
	return persona, nil
}
