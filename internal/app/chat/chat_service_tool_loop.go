package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type toolGuardKind string

const (
	toolGuardKindDuplicate   toolGuardKind = "duplicate_call"
	toolGuardKindLimit       toolGuardKind = "tool_limit"
	toolGuardKindMaxRounds   toolGuardKind = "max_tool_rounds"
	toolGuardKindForcedFinal toolGuardKind = "forced_final_answer"
)

type toolGuardEvent struct {
	Kind    toolGuardKind
	Tool    string
	Reason  string
	Details string
}

type toolGuardSummary struct {
	Events []toolGuardEvent
}

func (s *toolGuardSummary) Add(kind toolGuardKind, toolName, reason, details string) {
	if s == nil {
		return
	}
	s.Events = append(s.Events, toolGuardEvent{
		Kind:    kind,
		Tool:    toolName,
		Reason:  reason,
		Details: details,
	})
}

func (s toolGuardSummary) Empty() bool {
	return len(s.Events) == 0
}

func (s toolGuardSummary) Has(kind toolGuardKind) bool {
	for _, event := range s.Events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func (s toolGuardSummary) shouldForceFinalAnswer(policy ToolExecutionPolicy) bool {
	if !policy.ForceFinalAnswerAfterToolLimit {
		return false
	}
	return s.Has(toolGuardKindMaxRounds) || s.Has(toolGuardKindLimit) || s.Has(toolGuardKindDuplicate)
}

func (s toolGuardSummary) buildPromptText() string {
	if len(s.Events) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tool guard events in this turn:\n")
	for i, event := range s.Events {
		tool := event.Tool
		if strings.TrimSpace(tool) == "" {
			tool = "unknown"
		}
		b.WriteString(fmt.Sprintf("%d. kind=%s tool=%s reason=%s", i+1, event.Kind, tool, event.Reason))
		if strings.TrimSpace(event.Details) != "" {
			b.WriteString(" details=")
			b.WriteString(event.Details)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func buildToolLoopFallbackResponse(policy PromptGuardPolicy) string {
	if policy.TripletOutputEnabled {
		return strings.TrimSpace(
			"[Status]: Degraded / Tool Loop Stopped\n" +
				"[Analysis]: Repeated tool calls were blocked to prevent infinite recursion.\n" +
				"[Response]: 抱歉，我剛剛在工具呼叫流程卡住了，已自動停止重複呼叫。請再說一次你的需求，我會直接回覆重點，不再重複呼叫工具。",
		)
	}
	return "抱歉，我剛剛在工具呼叫流程卡住了，已自動停止重複呼叫。請再說一次你的需求，我會直接回覆重點。"
}

func buildDuplicateToolGuardResult(call domain.ToolCall) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Content: fmt.Sprintf(
			"Tool guard: duplicate call blocked for %q with identical input in this turn. "+
				"Do NOT call it again; continue with available results and answer the user directly.",
			call.Name,
		),
		IsError: true,
	}
}

func buildToolLimitGuardResult(call domain.ToolCall, limit int) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Content: fmt.Sprintf(
			"Tool guard: %q call limit reached (%d per turn). "+
				"Do NOT call this tool again in this turn; continue with available results and answer directly.",
			call.Name,
			limit,
		),
		IsError: true,
	}
}

func isToolGuardResultMessage(content string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(content)), "tool guard:")
}

func buildForcedFinalAnswerPrompt(summary toolGuardSummary) string {
	var b strings.Builder
	b.WriteString("[Tool guard continuation]\n")
	b.WriteString("Tool execution was intentionally limited to prevent loops. ")
	b.WriteString("You MUST provide the best possible final answer using only already available tool results and conversation context.\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT call any tool again.\n")
	b.WriteString("- If evidence is partial, state uncertainty briefly and still provide a concrete answer.\n")
	b.WriteString("- Do not ask the user to repeat unless absolutely necessary.\n")
	if details := summary.buildPromptText(); details != "" {
		b.WriteString("\n")
		b.WriteString(details)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *ChatService) forceFinalAnswerWithoutTools(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	summary toolGuardSummary,
) (string, int, error) {
	forcedPrompt := buildForcedFinalAnswerPrompt(summary)
	conv.Messages = append(conv.Messages, domain.Message{
		Role:    domain.RoleUser,
		Content: forcedPrompt,
	})
	defer func() {
		if len(conv.Messages) > 0 {
			conv.Messages = conv.Messages[:len(conv.Messages)-1]
		}
	}()

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return "", 0, fmt.Errorf("assemble forced final answer: %w", err)
	}
	s.logPromptSnapshot("stream-forced-final", conv.ChannelID, conv.PersonaID, conv.ID, assembled)

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeDisabled,
	})
	if err != nil {
		return "", 0, fmt.Errorf("forced final answer llm call: %w", err)
	}

	var fullResponse strings.Builder
	var tokensUsed int
	for chunk := range streamCh {
		if chunk.TokensUsed > 0 {
			tokensUsed += chunk.TokensUsed
		}
		if chunk.Error != nil {
			return "", tokensUsed, chunk.Error
		}
		if len(chunk.ToolCalls) > 0 {
			return "", tokensUsed, fmt.Errorf("forced final answer unexpectedly requested tool calls")
		}
		fullResponse.WriteString(chunk.Content)
	}

	return strings.TrimSpace(fullResponse.String()), tokensUsed, nil
}

// handleToolCallingStream handles the tool calling loop with streaming.
// When the LLM requests tool calls, this executes them and continues the conversation.
// Supports up to maxToolRounds of chained tool calls.
// multimodalHistory is non-nil when the conversation includes images; the continuation
// will use StreamChatMultimodal to preserve image context.
func (s *ChatService) handleToolCallingStream(
	ctx context.Context,
	streamCh <-chan llm.StreamChunk,
	conv *domain.Conversation,
	messageToStore, contextForLLM string,
	persona *domain.PersonaModule,
	personaID string,
	imageRefs []domain.ImageRef, // nil for text-only messages
	multimodalHistory []domain.MultimodalMessage, // nil for text-only; includes images for multimodal
) <-chan llm.StreamChunk {
	outputCh := make(chan llm.StreamChunk, 16)

	go func() {
		defer close(outputCh)

		state := newToolLoopState(s.toolExecutionPolicySnapshot(), s.assembler.PromptGuardPolicy())
		currentStream := streamCh
		state.baseMessageCount = len(conv.Messages)
		conv.AppendUserMessage(contextForLLM)
		defer func() {
			if state.baseMessageCount <= len(conv.Messages) {
				conv.Messages = conv.Messages[:state.baseMessageCount]
			}
		}()

		for round := 0; round <= state.toolPolicy.MaxToolRounds; round++ {
			pendingToolCalls, visibleRoundContent, ok := s.consumeRoundStream(currentStream, outputCh, state)
			if !ok {
				return
			}

			if len(pendingToolCalls) == 0 {
				if !state.hideAnalysis && visibleRoundContent != "" {
					outputCh <- llm.StreamChunk{Content: visibleRoundContent}
					state.finalTextSent = true
				}
				break
			}

			state.hadToolCallAttempt = true
			names := make([]string, len(pendingToolCalls))
			for i, c := range pendingToolCalls {
				names[i] = c.Name
			}
			logChat.Logf("tool calls (round %d/%d): %s", round+1, state.toolPolicy.MaxToolRounds, strings.Join(names, ", "))

			if round == state.toolPolicy.MaxToolRounds {
				state.guardSummary.Add(toolGuardKindMaxRounds, "", "max_tool_rounds_reached",
					fmt.Sprintf("max_tool_rounds=%d", state.toolPolicy.MaxToolRounds))
				s.incrementToolGuardMaxRounds()
				logChat.Warnf("max tool rounds (%d) reached, stopping", state.toolPolicy.MaxToolRounds)
				break
			}

			emitToolStatuses(outputCh, "tool_start", pendingToolCalls)
			executableCalls, guardResults := s.filterExecutableToolCalls(pendingToolCalls, personaID, round, state)
			nextStream, nextMultimodalHistory, ok := s.continueWithToolResults(
				ctx, conv, persona, personaID, pendingToolCalls, executableCalls, guardResults, multimodalHistory, outputCh, state, round,
			)
			if !ok {
				return
			}
			currentStream = nextStream
			multimodalHistory = nextMultimodalHistory
		}

		finalView := s.finalizeToolLoopResponse(ctx, conv, persona, outputCh, state)
		s.persistToolLoopConversation(ctx, conv, messageToStore, contextForLLM, persona, personaID, imageRefs, finalView, outputCh, state)

		// Send final Done signal to indicate stream completion
		outputCh <- llm.StreamChunk{Done: true}
	}()

	return outputCh
}
