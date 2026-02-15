package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
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

	// Build current user message parts with interleaved history images
	var userParts []domain.MessagePart
	if len(historyEntries) > 0 {
		// History header
		userParts = append(userParts, domain.MessagePart{Text: domain.HistoryContextMarker})
		// Each history entry: text part followed by its associated images
		for _, entry := range historyEntries {
			userParts = append(userParts, domain.MessagePart{Text: entry.FormattedText})
			for i := range entry.Images {
				logChat.Logf("history image: %s (%s, %.1fKB)", entry.Images[i].Filename, entry.Images[i].MimeType, float64(len(entry.Images[i].Data))/1024)
				userParts = append(userParts, domain.MessagePart{ImageData: &entry.Images[i]})
			}
		}
		// Current message section
		userParts = append(userParts, domain.MessagePart{Text: "\n" + domain.CurrentMessageMarker + "\n" + messageToStore})
	} else {
		// No history entries (DM or text-only history) — use flat context
		userParts = append(userParts, domain.MessagePart{Text: contextForLLM})
	}
	// Direct attachment images (always after current message text)
	for i := range currentImages {
		logChat.Logf("image: %s (%s, %.1fKB)", currentImages[i].Filename, currentImages[i].MimeType, float64(len(currentImages[i].Data))/1024)
		userParts = append(userParts, domain.MessagePart{ImageData: &currentImages[i]})
	}

	// Append current user message
	multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
		Role:  domain.RoleUser,
		Parts: userParts,
	})

	totalImages := len(currentImages)
	for _, mm := range multimodalHistory {
		for _, p := range mm.Parts {
			if p.ImageData != nil {
				totalImages++
			}
		}
	}
	logChat.Logf("multimodal history: %d messages, %d total images → LLM", len(multimodalHistory), totalImages)

	// Build raw LLM input for transparency
	var textOnlyMessages []domain.Message
	for _, mm := range multimodalHistory {
		var textParts []string
		for _, part := range mm.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.ImageData != nil {
				textParts = append(textParts, fmt.Sprintf("[Image: %s]", part.ImageData.Filename))
			}
		}
		textOnlyMessages = append(textOnlyMessages, domain.Message{
			Role:    mm.Role,
			Content: strings.Join(textParts, "\n"),
		})
	}
	rawLLMInput := formatLLMPrompt(assembled.SystemPrompt, textOnlyMessages)

	streamCh, err := s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
	if err != nil {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, personaID, rawLLMInput, currentImageRefs, multimodalHistory), nil
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
