package chat

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// HandleMessage processes a user message and returns the assistant response.
func (s *ChatService) HandleMessage(ctx context.Context, channelID, personaID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, channelID, personaID, "", userMessage)
}

// HandleMessageByConversationID processes a message targeting a specific conversation session.
func (s *ChatService) HandleMessageByConversationID(ctx context.Context, conversationID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, "", "", conversationID, userMessage)
}

func (s *ChatService) handleMessageInternal(ctx context.Context, channelID, personaID, conversationID, userMessage string) (string, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return "", err
	}
	keepRecentMessages := s.keepRecentMessagesSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentMessages); err != nil {
		return "", err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentMessages)

	contextForLLM := s.encodeUserMessageForLLM(ctx, conv.ChannelID, resolvedPersonaID, userMessage)
	conv.AppendUserMessage(contextForLLM)

	modelInfo := s.provider.ModelInfo()
	fmt.Printf("[ChatService] Assembling context - Provider: %s, SupportsThinking: %v\n",
		modelInfo.ProviderName, modelInfo.SupportsThinking)

	assembled, err := s.assembler.Assemble(persona, conv, modelInfo)
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("chat", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)
	rawInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	rawInputHuman := formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)

	fmt.Printf("[ChatService] Calling LLM - System prompt length: %d, Messages: %d\n",
		len(assembled.SystemPrompt), len(assembled.Messages))

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	fmt.Printf("[ChatService] LLM response received - Length: %d chars\n", len(resp.Content))

	view := s.prepareAssistantOutputForStorage(resp.Content)
	if len(conv.Messages) > 0 {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
	}
	userContent := s.encodeUserMessageForStorage(ctx, conv.ChannelID, resolvedPersonaID, userMessage, contextForLLM)
	assistantContent := s.encodeAssistantMessageForStorage(ctx, conv.ChannelID, persona, resolvedPersonaID, view.Content)
	conv.AppendUserMessageWithDisplay(userContent, userMessage)
	conv.AppendAssistantMessageWithDisplayAndRawViews(assistantContent, view.Display, rawInput, rawInputHuman, view.RawOutput)
	s.applyTokenAccounting(conv, resp.TokensUsed)

	if err := s.store.Save(ctx, conv); err != nil {
		return "", fmt.Errorf("save conversation [%s/%s]: %w", conv.ChannelID, resolvedPersonaID, err)
	}

	if view.Display != "" {
		return view.Display, nil
	}
	return view.Content, nil
}

// HandleMessageStream processes a user message and returns a streaming channel.
func (s *ChatService) HandleMessageStream(ctx context.Context, channelID, personaID, userMessage string) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithContext(ctx, channelID, personaID, userMessage, userMessage)
}

// HandleMessageStreamByConversationID processes a user message for a specific conversation session.
func (s *ChatService) HandleMessageStreamByConversationID(ctx context.Context, conversationID, userMessage string) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithContextInternal(ctx, "", "", conversationID, userMessage, userMessage)
}

// HandleMessageStreamWithContext processes a user message with separate display and storage versions.
// contextForLLM: the full message (with channel history) sent to LLM
// messageToStore: the clean message (without channel history markers) saved to conversation store
func (s *ChatService) HandleMessageStreamWithContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithContextInternal(ctx, channelID, personaID, "", contextForLLM, messageToStore)
}

func (s *ChatService) handleMessageStreamWithContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	keepRecentMessages := s.keepRecentMessagesSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentMessages); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentMessages)

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.memoryFlushRunner.Flush(ctx, MemoryFlushRequest{
			Conversation: conv,
			Persona:      persona,
			PersonaID:    resolvedPersonaID,
		}); err != nil {
			logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
		}
	}

	contextForLLM = s.ensureTOONContextForLLM(ctx, conv.ChannelID, resolvedPersonaID, contextForLLM, messageToStore)

	// Temporarily append the LLM context version for assembly
	conv.AppendUserMessage(contextForLLM)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("stream", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)
	initialRawInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	initialRawInputHuman := formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)

	// Remove the temporary message - we'll store the clean version later
	conv.Messages = conv.Messages[:len(conv.Messages)-1]

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	// Handle tool calling loop and wrap stream for persistence
	return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
		StreamCh:                streamCh,
		Conversation:            conv,
		MessageToStore:          messageToStore,
		ContextForLLM:           contextForLLM,
		Persona:                 persona,
		PersonaID:               resolvedPersonaID,
		InitialRawLLMInput:      initialRawInput,
		InitialRawLLMInputHuman: initialRawInputHuman,
	}), nil
}

// HandleMessageStreamWithImages processes a user message with images and returns a streaming channel.
func (s *ChatService) HandleMessageStreamWithImages(ctx context.Context, channelID, personaID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithImagesAndContext(ctx, channelID, personaID, userMessage, userMessage, images, nil)
}

// HandleMessageStreamWithImagesByConversationID processes a multimodal message for a specific conversation session.
func (s *ChatService) HandleMessageStreamWithImagesByConversationID(ctx context.Context, conversationID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithImagesAndContextInternal(ctx, "", "", conversationID, userMessage, userMessage, images, nil)
}

// HandleMessageStreamWithImagesAndContext processes a user message with images and separate display/storage versions.
// contextForLLM: the full message (with channel history) sent to LLM
// messageToStore: the clean message (without channel history markers) saved to conversation store
// images: direct attachment images — persisted to disk and sent to LLM
// historyEntries: structured channel history entries with per-message images (ephemeral, not persisted)
func (s *ChatService) HandleMessageStreamWithImagesAndContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithImagesAndContextInternal(ctx, channelID, personaID, "", contextForLLM, messageToStore, images, historyEntries)
}

func (s *ChatService) handleMessageStreamWithImagesAndContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	keepRecentMessages := s.keepRecentMessagesSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentMessages); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentMessages)

	contextForLLM = s.ensureTOONContextForLLM(ctx, conv.ChannelID, resolvedPersonaID, contextForLLM, messageToStore)

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.memoryFlushRunner.Flush(ctx, MemoryFlushRequest{
			Conversation: conv,
			Persona:      persona,
			PersonaID:    resolvedPersonaID,
		}); err != nil {
			logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
		}
	}

	// Save only direct attachment images to disk (not history entry images)
	var imageRefs []domain.ImageRef
	if s.imageStore != nil && len(images) > 0 {
		for _, img := range images {
			ref, err := s.imageStore.Save(img)
			if err != nil {
				logChat.Warnf("save image %s: %v (skipping)", img.Filename, err)
				continue
			}
			imageRefs = append(imageRefs, ref)
			logChat.Logf("image saved: %s → %s", img.Filename, ref.StorageKey)
		}
	}

	return s.multimodalRunner.StreamWithHistory(ctx, MultimodalRunRequest{
		Conversation:     conv,
		Persona:          persona,
		PersonaID:        resolvedPersonaID,
		ContextForLLM:    contextForLLM,
		MessageToStore:   messageToStore,
		CurrentImages:    images,
		HistoryEntries:   historyEntries,
		CurrentImageRefs: imageRefs,
	})
}

func (s *ChatService) preflightConversationRetention(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	keepRecentMessages int,
) error {
	if conv == nil {
		return nil
	}

	removedCount, addedDigests := compactHistoryWithKeepRecent(conv, persona, keepRecentMessages)
	if removedCount == 0 {
		return nil
	}
	if s.store == nil {
		return fmt.Errorf("conversation store is not configured")
	}

	logChat.Logf(
		"preflight retention: conversation_id=%s removed=%d kept=%d added_digests=%d digests_total=%d",
		conv.ID,
		removedCount,
		keepRecentMessages,
		addedDigests,
		len(conv.ContextDigests),
	)
	if err := s.store.Save(ctx, conv); err != nil {
		return fmt.Errorf("preflight retention save [%s/%s]: %w", conv.ChannelID, conv.PersonaID, err)
	}
	return nil
}
