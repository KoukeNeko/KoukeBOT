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

	modelInfo := s.provider.ModelInfo()
	fmt.Printf("[ChatService] Assembling context - Provider: %s, SupportsThinking: %v\n",
		modelInfo.ProviderName, modelInfo.SupportsThinking)

	assembled, err := s.assembler.Assemble(persona, conv, modelInfo)
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}

	fmt.Printf("[ChatService] Calling LLM - System prompt length: %d, Messages: %d\n",
		len(assembled.SystemPrompt), len(assembled.Messages))

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	fmt.Printf("[ChatService] LLM response received - Length: %d chars\n", len(resp.Content))

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

// HandleMessageStreamWithImages processes a user message with images and returns a streaming channel.
func (s *ChatService) HandleMessageStreamWithImages(ctx context.Context, channelID, personaID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Build multimodal message with text and images
	userParts := []domain.MessagePart{{Text: userMessage}}
	for _, img := range images {
		userParts = append(userParts, domain.MessagePart{ImageData: &img})
	}

	// Convert conversation history to multimodal format
	multimodalHistory := make([]domain.MultimodalMessage, 0, len(conv.Messages)+1)
	for _, msg := range conv.Messages {
		multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     []domain.MessagePart{{Text: msg.Content}},
			Timestamp: msg.Timestamp,
		})
	}
	// Append current user message with images
	multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
		Role:  domain.RoleUser,
		Parts: userParts,
	})

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

	streamCh, err := s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
	if err != nil {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
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
				// For now, store text-only representation (flatten images)
				conv.AppendUserMessage(userMessage) // TODO: enhance storage to support multimodal
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
