package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const (
	multimodalRetryReserveWindow     = 60 * time.Second
	multimodalNoDeadlineAttemptCap   = 4 * time.Minute
	multimodalNoDeadlineRetryCap     = 90 * time.Second
	multimodalMinimumRetryWindow     = 10 * time.Second
	multimodalRetryTriggerLogPreview = 220
)

// streamWithMultimodalHistory builds a multimodal history (loading images from disk for recent turns)
// and streams the response. `currentImages` are the images for the current message (may be nil for text-only follow-ups).
// `currentImageRefs` are the refs to persist in the conversation JSON.
func (s *ChatService) streamWithMultimodalHistory(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	personaID string,
	contextForLLM, messageToStore string,
	currentImages []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
	currentImageRefs ...domain.ImageRef,
) (<-chan llm.StreamChunk, error) {
	// Assemble budgeted text context first, including the current user turn for digest relevance.
	conv.AppendUserMessage(contextForLLM)
	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return nil, fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("multimodal-stream", conv.ChannelID, personaID, conv.ID, assembled)
	conv.Messages = conv.Messages[:len(conv.Messages)-1]

	// Convert assembler output to multimodal text history and drop the temporary current turn.
	baseMessages := assembled.Messages
	if n := len(baseMessages); n > 0 {
		last := baseMessages[n-1]
		if last.Role == domain.RoleUser && last.Content == contextForLLM {
			baseMessages = baseMessages[:n-1]
		}
	}
	multimodalHistory := buildTextMultimodalHistory(baseMessages)

	// Full payload: history text/images + current message + current attachments.
	fullUserParts := s.buildCurrentMultimodalUserParts(
		ctx,
		conv.ChannelID,
		personaID,
		contextForLLM,
		messageToStore,
		currentImages,
		historyEntries,
		true,
	)
	fullMultimodalHistory := appendCurrentMultimodalMessage(multimodalHistory, fullUserParts)
	fullRawInput := formatMultimodalLLMPrompt(assembled.SystemPrompt, fullMultimodalHistory)
	fullRawInputHuman := formatMultimodalLLMPromptHuman(assembled.SystemPrompt, fullMultimodalHistory)
	logChat.Logf(
		"multimodal history: mode=full messages=%d images=%d → LLM",
		len(fullMultimodalHistory),
		countMultimodalImages(fullMultimodalHistory),
	)

	initialCtx, initialCancel, retryBudget := deriveInitialMultimodalAttemptContext(ctx)
	streamCh, err := s.provider.StreamChatMultimodal(initialCtx, assembled.SystemPrompt, fullMultimodalHistory, assembled.Generation)
	if err == nil {
		streamCh = withStreamContextCancel(streamCh, initialCancel)
		return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
			StreamCh:                streamCh,
			Conversation:            conv,
			MessageToStore:          messageToStore,
			ContextForLLM:           contextForLLM,
			Persona:                 persona,
			PersonaID:               personaID,
			ImageRefs:               currentImageRefs,
			MultimodalHistory:       fullMultimodalHistory,
			InitialRawLLMInput:      fullRawInput,
			InitialRawLLMInputHuman: fullRawInputHuman,
		}), nil
	}
	if initialCancel != nil {
		initialCancel()
	}

	if !isMultimodalTimeoutLikeError(err) || ctx.Err() != nil {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	retryCtx, retryCancel, ok := deriveRetryMultimodalAttemptContext(ctx, retryBudget)
	if !ok {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	logChat.Warnf(
		"multimodal timeout detected; retry mode=reduced_context cause=%s",
		truncateLog(err.Error(), multimodalRetryTriggerLogPreview),
	)
	reducedUserParts := s.buildCurrentMultimodalUserParts(
		retryCtx,
		conv.ChannelID,
		personaID,
		contextForLLM,
		messageToStore,
		currentImages,
		nil,
		false,
	)
	reducedMultimodalHistory := appendCurrentMultimodalMessage(multimodalHistory, reducedUserParts)
	reducedRawInput := formatMultimodalLLMPrompt(assembled.SystemPrompt, reducedMultimodalHistory)
	reducedRawInputHuman := formatMultimodalLLMPromptHuman(assembled.SystemPrompt, reducedMultimodalHistory)
	logChat.Logf(
		"multimodal history: mode=reduced_context messages=%d images=%d → LLM",
		len(reducedMultimodalHistory),
		countMultimodalImages(reducedMultimodalHistory),
	)

	streamCh, err = s.provider.StreamChatMultimodal(retryCtx, assembled.SystemPrompt, reducedMultimodalHistory, assembled.Generation)
	if err != nil {
		if retryCancel != nil {
			retryCancel()
		}
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}
	streamCh = withStreamContextCancel(streamCh, retryCancel)

	return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
		StreamCh:                streamCh,
		Conversation:            conv,
		MessageToStore:          messageToStore,
		ContextForLLM:           contextForLLM,
		Persona:                 persona,
		PersonaID:               personaID,
		ImageRefs:               currentImageRefs,
		MultimodalHistory:       reducedMultimodalHistory,
		InitialRawLLMInput:      reducedRawInput,
		InitialRawLLMInputHuman: reducedRawInputHuman,
	}), nil
}

// streamWithNativeMultimodalHistory is the multimodal counterpart to HandleMessageStreamWithNativeHistory.
// Each ChannelHistoryEntry becomes its own role-based MultimodalMessage so that images
// are attributed to the correct speaker turn rather than all bundled into one user message.
func (s *ChatService) streamWithNativeMultimodalHistory(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	personaID string,
	ephemeralHistory []domain.Message,
	currentMessage, messageToStore string,
	currentImages []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
	currentImageRefs ...domain.ImageRef,
) (<-chan llm.StreamChunk, error) {
	// Temporarily append ephemeral history + current message so the assembler can
	// use the current turn for digest relevance scoring.
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
	s.logPromptSnapshot("multimodal-native-stream", conv.ChannelID, personaID, conv.ID, assembled)

	// Restore to stored state — ephemeral messages are never persisted.
	conv.Messages = conv.Messages[:savedLen]

	// Drop the ephemeral portion from the assembled output. The assembled slice
	// ends with: [...stored turns, ephemeral history turns, current user turn].
	// We strip these so we can re-append them as proper multimodal messages.
	ephemeralCount := len(ephemeralHistory) + 1 // +1 for the appended current message
	baseMessages := assembled.Messages
	if len(baseMessages) >= ephemeralCount {
		baseMessages = baseMessages[:len(baseMessages)-ephemeralCount]
	}
	multimodalHistory := buildTextMultimodalHistory(baseMessages)

	// Build ephemeral multimodal messages: one per history entry (with correct role + images)
	// followed by the current user message (with its direct attachments).
	ephemeralMM := buildEphemeralMultimodalMessages(historyEntries, currentMessage, currentImages)
	fullMultimodalHistory := append(multimodalHistory, ephemeralMM...)

	fullRawInput := formatMultimodalLLMPrompt(assembled.SystemPrompt, fullMultimodalHistory)
	fullRawInputHuman := formatMultimodalLLMPromptHuman(assembled.SystemPrompt, fullMultimodalHistory)
	logChat.Logf(
		"multimodal native history: mode=full messages=%d images=%d → LLM",
		len(fullMultimodalHistory),
		countMultimodalImages(fullMultimodalHistory),
	)

	initialCtx, initialCancel, retryBudget := deriveInitialMultimodalAttemptContext(ctx)
	streamCh, err := s.provider.StreamChatMultimodal(initialCtx, assembled.SystemPrompt, fullMultimodalHistory, assembled.Generation)
	if err == nil {
		streamCh = withStreamContextCancel(streamCh, initialCancel)
		return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
			StreamCh:                streamCh,
			Conversation:            conv,
			MessageToStore:          messageToStore,
			ContextForLLM:           currentMessage,
			Persona:                 persona,
			PersonaID:               personaID,
			ImageRefs:               currentImageRefs,
			MultimodalHistory:       fullMultimodalHistory,
			InitialRawLLMInput:      fullRawInput,
			InitialRawLLMInputHuman: fullRawInputHuman,
		}), nil
	}
	if initialCancel != nil {
		initialCancel()
	}

	if !isMultimodalTimeoutLikeError(err) || ctx.Err() != nil {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	retryCtx, retryCancel, ok := deriveRetryMultimodalAttemptContext(ctx, retryBudget)
	if !ok {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	logChat.Warnf(
		"multimodal native timeout; retry mode=reduced_context cause=%s",
		truncateLog(err.Error(), multimodalRetryTriggerLogPreview),
	)

	// Retry without history images — keep only the current message attachments.
	reducedMM := buildEphemeralMultimodalMessages(nil, currentMessage, currentImages)
	reducedMultimodalHistory := append(multimodalHistory, reducedMM...)
	reducedRawInput := formatMultimodalLLMPrompt(assembled.SystemPrompt, reducedMultimodalHistory)
	reducedRawInputHuman := formatMultimodalLLMPromptHuman(assembled.SystemPrompt, reducedMultimodalHistory)
	logChat.Logf(
		"multimodal native history: mode=reduced_context messages=%d images=%d → LLM",
		len(reducedMultimodalHistory),
		countMultimodalImages(reducedMultimodalHistory),
	)

	streamCh, err = s.provider.StreamChatMultimodal(retryCtx, assembled.SystemPrompt, reducedMultimodalHistory, assembled.Generation)
	if err != nil {
		if retryCancel != nil {
			retryCancel()
		}
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}
	streamCh = withStreamContextCancel(streamCh, retryCancel)

	return s.toolLoopRunner.Run(ctx, ToolLoopRunRequest{
		StreamCh:                streamCh,
		Conversation:            conv,
		MessageToStore:          messageToStore,
		ContextForLLM:           currentMessage,
		Persona:                 persona,
		PersonaID:               personaID,
		ImageRefs:               currentImageRefs,
		MultimodalHistory:       reducedMultimodalHistory,
		InitialRawLLMInput:      reducedRawInput,
		InitialRawLLMInputHuman: reducedRawInputHuman,
	}), nil
}

// buildEphemeralMultimodalMessages converts channel history entries into role-based
// MultimodalMessages and appends the current user message as the final turn.
func buildEphemeralMultimodalMessages(
	historyEntries []domain.ChannelHistoryEntry,
	currentMessage string,
	currentImages []domain.ImageInput,
) []domain.MultimodalMessage {
	result := make([]domain.MultimodalMessage, 0, len(historyEntries)+1)

	for _, entry := range historyEntries {
		if strings.TrimSpace(entry.FormattedText) == "" {
			continue
		}
		role := domain.RoleUser
		if entry.IsBot {
			role = domain.RoleAssistant
		}
		parts := []domain.MessagePart{{Text: entry.FormattedText}}
		for i := range entry.Images {
			logChat.Logf(
				"history image: %s (%s, %.1fKB)",
				entry.Images[i].Filename,
				entry.Images[i].MimeType,
				float64(len(entry.Images[i].Data))/1024,
			)
			parts = append(parts, domain.MessagePart{ImageData: &entry.Images[i]})
		}
		result = append(result, domain.MultimodalMessage{Role: role, Parts: parts})
	}

	// Current user message (text + direct attachments).
	currentParts := make([]domain.MessagePart, 0, len(currentImages)+1)
	if currentMessage != "" {
		currentParts = append(currentParts, domain.MessagePart{Text: currentMessage})
	}
	for i := range currentImages {
		logChat.Logf(
			"image: %s (%s, %.1fKB)",
			currentImages[i].Filename,
			currentImages[i].MimeType,
			float64(len(currentImages[i].Data))/1024,
		)
		currentParts = append(currentParts, domain.MessagePart{ImageData: &currentImages[i]})
	}
	if len(currentParts) > 0 {
		result = append(result, domain.MultimodalMessage{Role: domain.RoleUser, Parts: currentParts})
	}

	return result
}

func buildTextMultimodalHistory(messages []domain.Message) []domain.MultimodalMessage {
	result := make([]domain.MultimodalMessage, 0, len(messages))
	for _, msg := range messages {
		result = append(result, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     []domain.MessagePart{{Text: msg.Content}},
			Timestamp: msg.Timestamp,
		})
	}
	return result
}

func (s *ChatService) buildCurrentMultimodalUserParts(
	ctx context.Context,
	channelID string,
	personaID string,
	contextForLLM string,
	messageToStore string,
	currentImages []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
	includeHistory bool,
) []domain.MessagePart {
	parts := make([]domain.MessagePart, 0, len(historyEntries)+len(currentImages)+1)
	currentMessageEvent := strings.TrimSpace(s.encodeUserMessageForLLM(ctx, channelID, personaID, messageToStore))
	if currentMessageEvent == "" {
		currentMessageEvent = strings.TrimSpace(contextForLLM)
	}

	if includeHistory {
		for _, entry := range historyEntries {
			if strings.TrimSpace(entry.FormattedText) != "" {
				parts = append(parts, domain.MessagePart{Text: entry.FormattedText})
			}
			for i := range entry.Images {
				logChat.Logf(
					"history image: %s (%s, %.1fKB)",
					entry.Images[i].Filename,
					entry.Images[i].MimeType,
					float64(len(entry.Images[i].Data))/1024,
				)
				parts = append(parts, domain.MessagePart{ImageData: &entry.Images[i]})
			}
		}
	}

	if currentMessageEvent != "" {
		parts = append(parts, domain.MessagePart{Text: currentMessageEvent})
	}

	for i := range currentImages {
		logChat.Logf(
			"image: %s (%s, %.1fKB)",
			currentImages[i].Filename,
			currentImages[i].MimeType,
			float64(len(currentImages[i].Data))/1024,
		)
		parts = append(parts, domain.MessagePart{ImageData: &currentImages[i]})
	}

	return parts
}

func appendCurrentMultimodalMessage(base []domain.MultimodalMessage, parts []domain.MessagePart) []domain.MultimodalMessage {
	out := make([]domain.MultimodalMessage, len(base), len(base)+1)
	copy(out, base)
	out = append(out, domain.MultimodalMessage{
		Role:  domain.RoleUser,
		Parts: parts,
	})
	return out
}

func countMultimodalImages(messages []domain.MultimodalMessage) int {
	total := 0
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if part.ImageData != nil {
				total++
			}
		}
	}
	return total
}

func deriveInitialMultimodalAttemptContext(parent context.Context) (context.Context, context.CancelFunc, time.Duration) {
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return parent, nil, 0
		}
		if remaining > multimodalRetryReserveWindow+multimodalMinimumRetryWindow {
			attemptBudget := remaining - multimodalRetryReserveWindow
			attemptCtx, cancel := context.WithTimeout(parent, attemptBudget)
			return attemptCtx, cancel, multimodalRetryReserveWindow
		}
		return parent, nil, 0
	}

	attemptCtx, cancel := context.WithTimeout(parent, multimodalNoDeadlineAttemptCap)
	return attemptCtx, cancel, multimodalNoDeadlineRetryCap
}

func deriveRetryMultimodalAttemptContext(parent context.Context, preferredBudget time.Duration) (context.Context, context.CancelFunc, bool) {
	if parent.Err() != nil {
		return nil, nil, false
	}

	budget := preferredBudget
	if budget <= 0 {
		if _, ok := parent.Deadline(); ok {
			return nil, nil, false
		}
		budget = multimodalNoDeadlineRetryCap
	}

	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= multimodalMinimumRetryWindow {
			return nil, nil, false
		}
		if budget > remaining {
			budget = remaining
		}
	}

	if budget <= 0 {
		return nil, nil, false
	}
	retryCtx, cancel := context.WithTimeout(parent, budget)
	return retryCtx, cancel, true
}

func isMultimodalTimeoutLikeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "context canceled") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "client.timeout exceeded")
}

func withStreamContextCancel(streamCh <-chan llm.StreamChunk, cancel context.CancelFunc) <-chan llm.StreamChunk {
	if cancel == nil {
		return streamCh
	}
	out := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(out)
		defer cancel()
		for chunk := range streamCh {
			out <- chunk
		}
	}()
	return out
}
