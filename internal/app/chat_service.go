package app

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// ChatService orchestrates the full chat pipeline:
// receive message → load conversation → assemble context → call LLM → persist → return.
type ChatService struct {
	provider  llm.Provider
	assembler *ContextAssembler
	store     store.ConversationRepository
	personas  map[string]*domain.PersonaModule
}

func NewChatService(
	provider llm.Provider,
	assembler *ContextAssembler,
	convStore store.ConversationRepository,
	personas map[string]*domain.PersonaModule,
) *ChatService {
	return &ChatService{
		provider:  provider,
		assembler: assembler,
		store:     convStore,
		personas:  personas,
	}
}

// HandleMessage processes a user message and returns the assistant response.
func (s *ChatService) HandleMessage(ctx context.Context, channelID, personaID, userMessage string) (string, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return "", err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return "", fmt.Errorf("get conversation: %w", err)
	}

	conv.AppendUserMessage(userMessage)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	conv.AppendAssistantMessage(resp.Content)

	if err := s.store.Save(ctx, conv); err != nil {
		return "", fmt.Errorf("save conversation: %w", err)
	}

	return resp.Content, nil
}

// HandleMessageStream processes a user message and returns a streaming channel.
func (s *ChatService) HandleMessageStream(ctx context.Context, channelID, personaID, userMessage string) (<-chan llm.StreamChunk, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	conv.AppendUserMessage(userMessage)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
	})
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	// Wrap the stream to capture the full response for persistence
	outputCh := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(outputCh)
		var fullResponse string

		for chunk := range streamCh {
			outputCh <- chunk
			if chunk.Error != nil {
				return
			}
			fullResponse += chunk.Content
			if chunk.Done {
				conv.AppendAssistantMessage(fullResponse)
				_ = s.store.Save(ctx, conv) // best-effort persist
			}
		}
	}()

	return outputCh, nil
}

func (s *ChatService) resolvePersona(personaID string) (*domain.PersonaModule, error) {
	persona, exists := s.personas[personaID]
	if !exists {
		return nil, fmt.Errorf("persona %q not found", personaID)
	}
	return persona, nil
}
