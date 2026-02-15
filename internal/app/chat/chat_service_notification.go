package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// GeneratePersonaNotification generates a one-shot persona-aligned message
// without reading/writing conversation state and with tools disabled.
func (s *ChatService) GeneratePersonaNotification(ctx context.Context, personaID, channelID, userMessage string) (string, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	conv := &domain.Conversation{
		ID:        "",
		ChannelID: channelID,
		PersonaID: personaID,
		Messages: []domain.Message{
			{
				Role:      domain.RoleUser,
				Content:   userMessage,
				Timestamp: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return "", fmt.Errorf("assemble notification context: %w", err)
	}

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeDisabled,
	})
	if err != nil {
		return "", fmt.Errorf("generate notification: %w", err)
	}

	view := s.prepareAssistantOutputForStorage(resp.Content)
	if strings.TrimSpace(view.Display) != "" {
		return strings.TrimSpace(view.Display), nil
	}
	return strings.TrimSpace(view.Content), nil
}
