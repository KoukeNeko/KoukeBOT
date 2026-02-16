package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func isForcedFinalOutputValid(raw string, policy PromptGuardPolicy) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if !policy.TripletOutputEnabled {
		return true
	}
	_, ok := parseTripletSections(trimmed)
	return ok
}

func shouldRepairForcedFinalOutput(raw string, policy PromptGuardPolicy) bool {
	if !policy.TripletOutputEnabled {
		return false
	}
	_, ok := parseTripletSections(strings.TrimSpace(raw))
	return !ok
}

func buildForcedFinalRepairPrompt(previousResponse string) string {
	var b strings.Builder
	b.WriteString("[Forced final repair]\n")
	b.WriteString("Your previous answer failed the required output contract.\n")
	b.WriteString("Rewrite the same intent into a valid final answer.\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT call any tool.\n")
	b.WriteString("- Keep the same meaning; only fix structure/clarity.\n")
	b.WriteString("- Output must contain exactly these sections in order: [Status], [Analysis], [Response].\n")
	b.WriteString("- [Response] must be non-empty and user-facing.\n")
	b.WriteString("\nPrevious output:\n")
	b.WriteString(previousResponse)
	return strings.TrimSpace(b.String())
}

func (s *ChatService) regenerateForcedFinalOnceWithoutTools(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	previousResponse string,
) (string, int, string, string, error) {
	repairPrompt := buildForcedFinalRepairPrompt(previousResponse)
	conv.Messages = append(conv.Messages, domain.Message{
		Role:    domain.RoleUser,
		Content: repairPrompt,
	})
	defer func() {
		if len(conv.Messages) > 0 {
			conv.Messages = conv.Messages[:len(conv.Messages)-1]
		}
	}()

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return "", 0, "", "", fmt.Errorf("assemble forced final repair: %w", err)
	}
	s.logPromptSnapshot("stream-forced-final-repair", conv.ChannelID, conv.PersonaID, conv.ID, assembled)
	rawInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	rawInputHuman := formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeDisabled,
	})
	if err != nil {
		return "", 0, rawInput, rawInputHuman, fmt.Errorf("forced final repair llm call: %w", err)
	}

	var fullResponse strings.Builder
	var tokensUsed int
	for chunk := range streamCh {
		if chunk.TokensUsed > 0 {
			tokensUsed += chunk.TokensUsed
		}
		if chunk.Error != nil {
			return "", tokensUsed, rawInput, rawInputHuman, chunk.Error
		}
		if len(chunk.ToolCalls) > 0 {
			return "", tokensUsed, rawInput, rawInputHuman, fmt.Errorf("forced final repair unexpectedly requested tool calls")
		}
		fullResponse.WriteString(chunk.Content)
	}

	return strings.TrimSpace(fullResponse.String()), tokensUsed, rawInput, rawInputHuman, nil
}
