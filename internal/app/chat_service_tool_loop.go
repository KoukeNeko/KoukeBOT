package app

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
	rawLLMInput string,
	imageRefs []domain.ImageRef, // nil for text-only messages
	multimodalHistory []domain.MultimodalMessage, // nil for text-only; includes images for multimodal
) <-chan llm.StreamChunk {
	outputCh := make(chan llm.StreamChunk, 16)

	go func() {
		defer close(outputCh)

		toolPolicy := s.toolExecutionPolicySnapshot()
		var executedToolCalls []domain.ToolCall
		var allToolResultsText string
		var totalTokens int
		toolCallAttempts := make(map[string]int)
		toolCountsByName := make(map[string]int)
		emittedToolErrors := make(map[string]struct{})
		hadToolCallAttempt := false
		guardSummary := toolGuardSummary{}
		promptPolicy := s.assembler.PromptGuardPolicy()
		hideAnalysis := promptPolicy.TripletOutputEnabled && promptPolicy.AnalysisVisibility == AnalysisVisibilityHidden

		currentStream := streamCh
		var fullResponse string
		finalTextSent := false
		baseMessageCount := len(conv.Messages)
		conv.AppendUserMessage(contextForLLM)
		defer func() {
			if baseMessageCount <= len(conv.Messages) {
				conv.Messages = conv.Messages[:baseMessageCount]
			}
		}()

		for round := 0; round <= toolPolicy.MaxToolRounds; round++ {
			var pendingToolCalls []domain.ToolCall
			fullResponse = ""
			var visibleRoundContent strings.Builder

			// Consume stream
			for chunk := range currentStream {
				if chunk.TokensUsed > 0 {
					totalTokens += chunk.TokensUsed
				}
				if len(chunk.ToolCalls) > 0 {
					pendingToolCalls = chunk.ToolCalls
					outputCh <- chunk
					continue
				}
				if chunk.Error != nil {
					outputCh <- chunk
					return
				}
				fullResponse += chunk.Content
				if !hideAnalysis && chunk.Content != "" {
					visibleRoundContent.WriteString(chunk.Content)
				}
			}

			// No tool calls → we have the final response
			if len(pendingToolCalls) == 0 {
				if !hideAnalysis && visibleRoundContent.Len() > 0 {
					outputCh <- llm.StreamChunk{Content: visibleRoundContent.String()}
					finalTextSent = true
				}
				break
			}

			// Log tool call names
			names := make([]string, len(pendingToolCalls))
			for i, c := range pendingToolCalls {
				names[i] = c.Name
			}
			logChat.Logf("tool calls (round %d/%d): %s", round+1, toolPolicy.MaxToolRounds, strings.Join(names, ", "))
			hadToolCallAttempt = true

			// Max rounds reached → force finalize on next step.
			if round == toolPolicy.MaxToolRounds {
				guardSummary.Add(toolGuardKindMaxRounds, "", "max_tool_rounds_reached",
					fmt.Sprintf("max_tool_rounds=%d", toolPolicy.MaxToolRounds))
				s.incrementToolGuardMaxRounds()
				logChat.Warnf("max tool rounds (%d) reached, stopping", toolPolicy.MaxToolRounds)
				break
			}

			// Send tool execution start status for each tool
			for _, call := range pendingToolCalls {
				outputCh <- llm.StreamChunk{
					ToolStatus: "tool_start",
					ToolName:   call.Name,
					ToolInput:  call.Input,
				}
			}

			// Execute tools with duplicate-call guard to prevent endless loops.
			executableCalls := make([]domain.ToolCall, 0, len(pendingToolCalls))
			toolResults := make([]domain.ToolResult, 0, len(pendingToolCalls))
			for _, call := range pendingToolCalls {
				if call.Name == "web_search" && toolCountsByName[call.Name] >= toolPolicy.MaxWebSearchCallsPerTurn {
					guardSummary.Add(
						toolGuardKindLimit,
						call.Name,
						"tool_call_limit_reached",
						fmt.Sprintf("max_web_search_calls_per_turn=%d", toolPolicy.MaxWebSearchCallsPerTurn),
					)
					s.incrementToolGuardLimitBlocked()
					toolResults = append(toolResults, buildToolLimitGuardResult(call, toolPolicy.MaxWebSearchCallsPerTurn))
					logChat.Warnf("tool loop guard blocked limit (round %d): %s%s", round+1, call.Name, summarizeToolTarget(call, personaID))
					continue
				}

				signature := toolCallSignature(call)
				if toolCallAttempts[signature] >= 1 {
					guardSummary.Add(toolGuardKindDuplicate, call.Name, "duplicate_tool_call_blocked", "")
					s.incrementToolGuardDuplicateBlocked()
					toolResults = append(toolResults, buildDuplicateToolGuardResult(call))
					logChat.Warnf("tool loop guard blocked duplicate call (round %d): %s%s", round+1, call.Name, summarizeToolTarget(call, personaID))
					continue
				}
				toolCallAttempts[signature]++
				toolCountsByName[call.Name]++
				executableCalls = append(executableCalls, call)
			}

			if len(executableCalls) > 0 {
				executed := s.executeTools(ctx, executableCalls, personaID)
				toolResults = append(toolResults, executed...)
				executedToolCalls = append(executedToolCalls, executableCalls...)
			}
			toolCallMap := make(map[string]domain.ToolCall, len(pendingToolCalls))
			for _, call := range pendingToolCalls {
				toolCallMap[call.ID] = call
			}
			for _, result := range toolResults {
				if !result.IsError {
					continue
				}
				if isToolGuardResultMessage(result.Content) {
					// Guard-triggered tool errors are internal control signals.
					// Keep them in backend logs/context, but do not surface to Discord users.
					continue
				}
				call := toolCallMap[result.ToolCallID]
				if toolPolicy.DiscordShowInternalToolErrors {
					errSummary := truncateLog(result.Content, 240)
					dedupeKey := call.Name + "|" + toolCallSignature(call) + "|" + errSummary
					if _, exists := emittedToolErrors[dedupeKey]; exists {
						continue
					}
					emittedToolErrors[dedupeKey] = struct{}{}
					outputCh <- llm.StreamChunk{
						ToolStatus: "tool_error",
						ToolName:   call.Name,
						ToolInput:  call.Input,
						ToolError:  errSummary,
					}
				}
			}

			// Send tool execution done status
			outputCh <- llm.StreamChunk{ToolStatus: "tool_done"}

			// Build tool results message for LLM
			toolResultsText := formatToolResults(pendingToolCalls, toolResults)
			allToolResultsText += toolResultsText

			// Append assistant's tool call request to conversation
			conv.AppendAssistantMessage("[Tool calls executed]")

			// Append tool results as user message
			conv.Messages = append(conv.Messages, domain.Message{
				Role:    domain.RoleUser,
				Content: toolResultsText,
			})

			// Also append to multimodal history if in multimodal mode
			if multimodalHistory != nil {
				multimodalHistory = append(multimodalHistory,
					domain.MultimodalMessage{
						Role:  domain.RoleAssistant,
						Parts: []domain.MessagePart{{Text: "[Tool calls executed]"}},
					},
					domain.MultimodalMessage{
						Role:  domain.RoleUser,
						Parts: []domain.MessagePart{{Text: toolResultsText}},
					},
				)
			}

			// Continue conversation with tool results
			logChat.Logf("continuing with tool results → LLM (round %d)", round+1)
			assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("assemble context after tools: %w", err), Done: true}
				return
			}
			s.logPromptSnapshot("stream-continuation", conv.ChannelID, conv.PersonaID, conv.ID, assembled)

			// Use multimodal continuation when images are present to preserve image context
			if multimodalHistory != nil {
				currentStream, err = s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
			} else {
				currentStream, err = s.provider.StreamChat(ctx, llm.ChatRequest{
					SystemPrompt: assembled.SystemPrompt,
					Messages:     assembled.Messages,
					Generation:   assembled.Generation,
					ToolMode:     llm.ToolModeNormal,
				})
			}
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("llm continuation: %w", err), Done: true}
				return
			}
		}

		if strings.TrimSpace(fullResponse) == "" && hadToolCallAttempt && guardSummary.shouldForceFinalAnswer(toolPolicy) {
			guardSummary.Add(toolGuardKindForcedFinal, "", "tool_guard_forced_final_answer", "")
			forcedResponse, forcedTokens, err := s.forceFinalAnswerWithoutTools(ctx, conv, persona, guardSummary)
			totalTokens += forcedTokens
			if err != nil {
				s.incrementToolForcedFinalFailed()
				logChat.Warnf("forced final answer failed: %v", err)
			} else if strings.TrimSpace(forcedResponse) != "" {
				s.incrementToolForcedFinalSuccess()
				fullResponse = forcedResponse
				if !hideAnalysis {
					outputCh <- llm.StreamChunk{Content: forcedResponse}
					finalTextSent = true
				}
				logChat.Logf("tool loop forced final answer generated after %d tool call(s)", len(executedToolCalls))
			}
		}

		if strings.TrimSpace(fullResponse) == "" && hadToolCallAttempt {
			s.incrementToolLoopFallback()
			fullResponse = buildToolLoopFallbackResponse(promptPolicy)
			logChat.Warnf("tool loop fallback response generated after %d tool call(s)", len(executedToolCalls))
		}

		finalView := s.prepareAssistantOutputForStorage(fullResponse)

		if hideAnalysis && finalView.Display != "" {
			outputCh <- llm.StreamChunk{Content: finalView.Display}
			finalTextSent = true
		}
		if !hideAnalysis && !finalTextSent && finalView.Display != "" {
			outputCh <- llm.StreamChunk{Content: finalView.Display}
			finalTextSent = true
		}

		// Save conversation and emit final signals
		if len(executedToolCalls) > 0 {
			// Tool calls were executed — append annotation and save
			toolUsageAnnotation := formatToolUsageAnnotation(executedToolCalls)
			display := finalView.Display
			if display == "" {
				display = finalView.Content
			}
			finalResponseWithAnnotation := display + "\n\n" + toolUsageAnnotation

			// Stream the annotation as a separate chunk
			outputCh <- llm.StreamChunk{Content: "\n\n" + toolUsageAnnotation}

			// Store final conversation with raw LLM data
			rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s",
				allToolResultsText, fullResponse)
			if baseMessageCount <= len(conv.Messages) {
				conv.Messages = conv.Messages[:baseMessageCount]
			}
			if len(imageRefs) > 0 {
				conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
			} else {
				conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			}
			content := finalView.Content
			if content == "" {
				content = strings.TrimSpace(fullResponse)
			}
			conv.AppendAssistantMessageWithDisplayAndRaw(content, finalResponseWithAnnotation, rawLLMInput, rawLLMOutput)
			s.applyTokenAccounting(conv, totalTokens)
			if err := s.store.Save(ctx, conv); err != nil {
				logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
			}

		} else {
			// No tool calls - store conversation normally with raw LLM data
			if fullResponse != "" || messageToStore != "" {
				if baseMessageCount <= len(conv.Messages) {
					conv.Messages = conv.Messages[:baseMessageCount]
				}
				if len(imageRefs) > 0 {
					conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
				} else {
					conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
				}
				conv.AppendAssistantMessageWithDisplayAndRaw(finalView.Content, finalView.Display, rawLLMInput, finalView.RawOutput)
				s.applyTokenAccounting(conv, totalTokens)
				if err := s.store.Save(ctx, conv); err != nil {
					logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
				}
			}
		}

		// Send final Done signal to indicate stream completion
		outputCh <- llm.StreamChunk{Done: true}
	}()

	return outputCh
}
