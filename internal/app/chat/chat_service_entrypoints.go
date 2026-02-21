package chat

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// popLastMessage removes the last element from a message slice.
// Used to discard the temporary LLM-context message added before assembly,
// leaving the slice in its pre-assembly state for clean storage.
// Returns the slice unchanged if it is empty.
func popLastMessage(msgs []domain.Message) []domain.Message {
	if len(msgs) == 0 {
		return msgs
	}
	return msgs[:len(msgs)-1]
}

// HandleMessage processes a user message and returns the assistant response.
func (s *ChatService) HandleMessage(ctx context.Context, channelID, personaID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, channelID, personaID, "", userMessage)
}

// HandleMessageByConversationID processes a message targeting a specific conversation session.
func (s *ChatService) HandleMessageByConversationID(ctx context.Context, conversationID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, "", "", conversationID, userMessage)
}

// handleMessageInternal is the shared implementation for synchronous (non-streaming) chat.
// It resolves the conversation, assembles the prompt, calls the LLM, and persists the result.
// Callers choose between channel+persona routing or direct conversation-ID routing
// by leaving the unused identifiers empty.
func (s *ChatService) handleMessageInternal(ctx context.Context, channelID, personaID, conversationID, userMessage string) (string, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return "", err
	}
	keepRecentUserTurns := s.keepRecentUserTurnsSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentUserTurns); err != nil {
		return "", err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)

	reqCtx, _ := llm.GetRequestContext(ctx)
	senderID := reqCtx.UserID

	contextForLLM := s.encodeUserMessageForLLM(ctx, conv.ChannelID, resolvedPersonaID, userMessage)
	conv.AppendUserMessage(contextForLLM)

	modelInfo := s.provider.ModelInfo()
	logChat.Logf("assembling context: provider=%s supports_thinking=%v", modelInfo.ProviderName, modelInfo.SupportsThinking)

	assembled, err := s.assembler.Assemble(persona, conv, modelInfo)
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("chat", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)
	rawInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	rawInputHuman := formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)

	logChat.Logf("calling LLM: system_prompt_len=%d messages=%d", len(assembled.SystemPrompt), len(assembled.Messages))

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	logChat.Logf("LLM response received: chars=%d", len(resp.Content))

	view := s.prepareAssistantOutputForStorage(resp.Content)
	conv.Messages = popLastMessage(conv.Messages)
	userContent := s.encodeUserMessageForStorage(ctx, conv.ChannelID, resolvedPersonaID, userMessage, contextForLLM)
	assistantContent := s.encodeAssistantMessageForStorage(ctx, conv.ChannelID, persona, resolvedPersonaID, view.Content)
	conv.AppendUserMessageWithDisplay(userContent, userMessage)
	conv.SetLastMessageSenderID(senderID)
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

// handleMessageStreamWithContextInternal is the shared implementation for text-only streaming chat.
// It resolves the conversation, runs retention and memory flush preflight, assembles the prompt,
// opens a streaming LLM call, and hands off to the tool-loop runner for continuation.
func (s *ChatService) handleMessageStreamWithContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	keepRecentUserTurns := s.keepRecentUserTurnsSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentUserTurns); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)

	s.maybeFlushMemory(ctx, conv, persona, resolvedPersonaID)

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

	// Remove the temporary LLM-context message — we'll store the clean version later.
	conv.Messages = popLastMessage(conv.Messages)

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

// handleMessageStreamWithImagesAndContextInternal is the shared implementation for multimodal streaming chat.
// It follows the same resolve → retention → flush → assemble flow as the text-only variant,
// but additionally persists direct attachment images and delegates to the multimodal runner
// so that image data is included in the LLM request.
func (s *ChatService) handleMessageStreamWithImagesAndContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	keepRecentUserTurns := s.keepRecentUserTurnsSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentUserTurns); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)

	contextForLLM = s.ensureTOONContextForLLM(ctx, conv.ChannelID, resolvedPersonaID, contextForLLM, messageToStore)
	s.maybeFlushMemory(ctx, conv, persona, resolvedPersonaID)

	// Save only direct attachment images to disk (history-entry images are ephemeral).
	imageRefs := s.saveDirectAttachmentImages(images)

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

// HandleMessageStreamWithNativeHistory processes a Discord message using native role-based
// channel history instead of the TOON ctx bundle. ephemeralHistory contains the
// channel history as real user/assistant turns; currentMessage is the plain current
// user text; messageToStore is the clean version saved to the conversation store.
func (s *ChatService) HandleMessageStreamWithNativeHistory(
	ctx context.Context,
	channelID, personaID string,
	ephemeralHistory []domain.Message,
	currentMessage, messageToStore string,
) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, "")
	if err != nil {
		return nil, err
	}
	keepRecentUserTurns := s.keepRecentUserTurnsSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentUserTurns); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)

	s.maybeFlushMemory(ctx, conv, persona, resolvedPersonaID)

	// Temporarily prepend ephemeral channel history + current message for assembly.
	// These are NOT persisted — they are removed immediately after Assemble.
	savedLen := len(conv.Messages)
	for _, msg := range ephemeralHistory {
		conv.Messages = append(conv.Messages, msg)
	}
	conv.AppendUserMessage(currentMessage)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		conv.Messages = conv.Messages[:savedLen]
		return nil, fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("stream-native", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)
	initialRawInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	initialRawInputHuman := formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)

	// Restore to stored state before streaming.
	conv.Messages = conv.Messages[:savedLen]

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
		StreamCh:                streamCh,
		Conversation:            conv,
		MessageToStore:          messageToStore,
		ContextForLLM:           currentMessage,
		Persona:                 persona,
		PersonaID:               resolvedPersonaID,
		InitialRawLLMInput:      initialRawInput,
		InitialRawLLMInputHuman: initialRawInputHuman,
	}), nil
}

// HandleMessageStreamWithNativeHistoryAndImages is the multimodal variant of
// HandleMessageStreamWithNativeHistory. Each history entry becomes its own
// role-based MultimodalMessage so images are attributed to the correct turn.
func (s *ChatService) HandleMessageStreamWithNativeHistoryAndImages(
	ctx context.Context,
	channelID, personaID string,
	ephemeralHistory []domain.Message,
	currentMessage, messageToStore string,
	images []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, "")
	if err != nil {
		return nil, err
	}
	keepRecentUserTurns := s.keepRecentUserTurnsSnapshot()
	if err := s.preflightConversationRetention(ctx, conv, persona, keepRecentUserTurns); err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokensWithRecentCap(conv, keepRecentUserTurns)

	s.maybeFlushMemory(ctx, conv, persona, resolvedPersonaID)

	// Save only direct attachment images to disk (history-entry images are ephemeral).
	imageRefs := s.saveDirectAttachmentImages(images)

	return s.streamWithNativeMultimodalHistory(
		ctx, conv, persona, resolvedPersonaID,
		ephemeralHistory, currentMessage, messageToStore,
		images, historyEntries, imageRefs...)
}

// maybeFlushMemory triggers a memory flush if the conversation is approaching the context limit.
// Errors are logged but do not abort the request — losing a flush is less harmful than dropping the message.
func (s *ChatService) maybeFlushMemory(ctx context.Context, conv *domain.Conversation, persona *domain.PersonaModule, personaID string) {
	if s.contextManager == nil || !s.contextManager.ShouldFlush(conv) {
		return
	}
	if err := s.memoryFlushRunner.Flush(ctx, MemoryFlushRequest{
		Conversation: conv,
		Persona:      persona,
		PersonaID:    personaID,
	}); err != nil {
		logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
	}
}

// saveDirectAttachmentImages persists the provided images to disk and returns their storage references.
// Images that fail to save are skipped with a warning rather than aborting the request,
// because losing an image reference is less harmful than dropping the entire message.
// History-entry images must NOT be passed here — only direct attachments should be persisted.
func (s *ChatService) saveDirectAttachmentImages(images []domain.ImageInput) []domain.ImageRef {
	if s.imageStore == nil || len(images) == 0 {
		return nil
	}
	refs := make([]domain.ImageRef, 0, len(images))
	for _, img := range images {
		ref, err := s.imageStore.Save(img)
		if err != nil {
			logChat.Warnf("save image %s: %v (skipping)", img.Filename, err)
			continue
		}
		refs = append(refs, ref)
		logChat.Logf("image saved: %s → %s", img.Filename, ref.StorageKey)
	}
	return refs
}

// preflightConversationRetention compacts the conversation history before a new LLM call.
// It removes older turns beyond keepRecentUserTurns, folds them into context digests,
// and persists the trimmed conversation. Returns nil (no-op) when no compaction is needed.
func (s *ChatService) preflightConversationRetention(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	keepRecentUserTurns int,
) error {
	if conv == nil {
		return nil
	}

	removedCount, addedDigests := compactHistoryWithKeepRecent(conv, persona, keepRecentUserTurns)
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
		keepRecentUserTurns,
		addedDigests,
		len(conv.ContextDigests),
	)
	if err := s.store.Save(ctx, conv); err != nil {
		return fmt.Errorf("preflight retention save [%s/%s]: %w", conv.ChannelID, conv.PersonaID, err)
	}
	return nil
}
