package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

type toolLoopState struct {
	toolPolicy   ToolExecutionPolicy
	promptPolicy PromptGuardPolicy
	hideAnalysis bool

	executedToolCalls    []domain.ToolCall
	allToolResultsText   string
	totalTokens          int
	toolCallAttempts     map[string]int
	toolCountsByName     map[string]int
	emittedToolErrors    map[string]struct{}
	hadToolCallAttempt   bool
	guardSummary         toolGuardSummary
	fallbackCause        string
	fullResponse         string
	finalTextSent        bool
	baseMessageCount     int
	lastRawLLMInput      string
	lastRawLLMInputHuman string
}

func newToolLoopState(toolPolicy ToolExecutionPolicy, promptPolicy PromptGuardPolicy) *toolLoopState {
	return &toolLoopState{
		toolPolicy:        toolPolicy,
		promptPolicy:      promptPolicy,
		hideAnalysis:      promptPolicy.TripletOutputEnabled && promptPolicy.AnalysisVisibility == AnalysisVisibilityHidden,
		toolCallAttempts:  make(map[string]int),
		toolCountsByName:  make(map[string]int),
		emittedToolErrors: make(map[string]struct{}),
	}
}

func (s *ChatService) consumeRoundStream(
	currentStream <-chan llm.StreamChunk,
	outputCh chan<- llm.StreamChunk,
	state *toolLoopState,
) ([]domain.ToolCall, string, bool) {
	var pendingToolCalls []domain.ToolCall
	state.fullResponse = ""
	var visibleRoundContent strings.Builder

	for chunk := range currentStream {
		if chunk.TokensUsed > 0 {
			state.totalTokens += chunk.TokensUsed
		}
		if len(chunk.ToolCalls) > 0 {
			pendingToolCalls = chunk.ToolCalls
			outputCh <- chunk
			continue
		}
		if chunk.Error != nil {
			outputCh <- chunk
			return nil, "", false
		}
		state.fullResponse += chunk.Content
		if !state.hideAnalysis && chunk.Content != "" {
			visibleRoundContent.WriteString(chunk.Content)
		}
	}

	return pendingToolCalls, visibleRoundContent.String(), true
}

func (s *ChatService) filterExecutableToolCalls(
	pendingToolCalls []domain.ToolCall,
	personaID string,
	round int,
	state *toolLoopState,
) ([]domain.ToolCall, []domain.ToolResult) {
	executableCalls := make([]domain.ToolCall, 0, len(pendingToolCalls))
	guardResults := make([]domain.ToolResult, 0, len(pendingToolCalls))

	for _, call := range pendingToolCalls {
		// Hard guard: session_status can be called at most once per turn, regardless of ToolGuardEnabled.
		if call.Name == "session_status" && state.toolCountsByName[call.Name] >= 1 {
			state.guardSummary.Add(
				toolGuardKindDuplicate,
				call.Name,
				"session_status_already_called_in_turn",
				"max_session_status_calls_per_turn=1",
			)
			s.incrementToolGuardDuplicateBlocked()
			guardResults = append(guardResults, buildSessionStatusRepeatGuardResult(call))
			logChat.Warnf(
				"%s [SESSION_STATUS_REPEAT] round=%d tool=%s%s",
				toolLogPrefixGuard,
				round+1,
				call.Name,
				summarizeToolTarget(call, personaID),
			)
			continue
		}

		if !state.toolPolicy.ToolGuardEnabled {
			signature := toolCallSignature(call)
			state.toolCallAttempts[signature]++
			state.toolCountsByName[call.Name]++
			executableCalls = append(executableCalls, call)
			continue
		}

		if call.Name == "web_search" && state.toolCountsByName[call.Name] >= state.toolPolicy.MaxWebSearchCallsPerTurn {
			state.guardSummary.Add(
				toolGuardKindLimit,
				call.Name,
				"tool_call_limit_reached",
				fmt.Sprintf("max_web_search_calls_per_turn=%d", state.toolPolicy.MaxWebSearchCallsPerTurn),
			)
			s.incrementToolGuardLimitBlocked()
			guardResults = append(guardResults, buildToolLimitGuardResult(call, state.toolPolicy.MaxWebSearchCallsPerTurn))
			logChat.Warnf("%s [LIMIT] round=%d tool=%s%s", toolLogPrefixGuard, round+1, call.Name, summarizeToolTarget(call, personaID))
			continue
		}

		signature := toolCallSignature(call)
		if state.toolCallAttempts[signature] >= 1 {
			state.guardSummary.Add(toolGuardKindDuplicate, call.Name, "duplicate_tool_call_blocked", "")
			s.incrementToolGuardDuplicateBlocked()
			guardResults = append(guardResults, buildDuplicateToolGuardResult(call))
			logChat.Warnf("%s [DUPLICATE] round=%d tool=%s%s", toolLogPrefixGuard, round+1, call.Name, summarizeToolTarget(call, personaID))
			continue
		}

		state.toolCallAttempts[signature]++
		state.toolCountsByName[call.Name]++
		executableCalls = append(executableCalls, call)
	}

	return executableCalls, guardResults
}

func emitToolStatuses(outputCh chan<- llm.StreamChunk, status string, calls []domain.ToolCall) {
	if status == "tool_start" {
		for _, call := range calls {
			outputCh <- llm.StreamChunk{
				ToolStatus: "tool_start",
				ToolName:   call.Name,
				ToolInput:  call.Input,
			}
		}
		return
	}
	outputCh <- llm.StreamChunk{ToolStatus: status}
}

func (s *ChatService) emitToolErrors(
	outputCh chan<- llm.StreamChunk,
	pendingToolCalls []domain.ToolCall,
	toolResults []domain.ToolResult,
	state *toolLoopState,
) {
	if !state.toolPolicy.DiscordShowInternalToolErrors {
		return
	}

	toolCallMap := make(map[string]domain.ToolCall, len(pendingToolCalls))
	for _, call := range pendingToolCalls {
		toolCallMap[call.ID] = call
	}

	for _, result := range toolResults {
		if !result.IsError {
			continue
		}

		call := toolCallMap[result.ToolCallID]
		if isToolGuardResultMessage(result.Content) {
			guardDetail := strings.TrimSpace(result.Content)
			if guardDetail == "" {
				guardDetail = "tool guard triggered without details"
			}
			dedupeKey := "guard|" + call.Name + "|" + toolCallSignature(call) + "|" + guardDetail
			if _, exists := state.emittedToolErrors[dedupeKey]; exists {
				continue
			}
			state.emittedToolErrors[dedupeKey] = struct{}{}
			logChat.Warnf(
				"%s [EMIT] tool=%s%s detail=%s",
				toolLogPrefixGuard,
				call.Name,
				summarizeToolTarget(call, "unknown"),
				singleLineToolLog(guardDetail),
			)
			outputCh <- llm.StreamChunk{
				ToolStatus: "tool_guard",
				ToolName:   call.Name,
				ToolInput:  call.Input,
				ToolError:  guardDetail,
			}
			continue
		}

		errSummary := strings.TrimSpace(result.Content)
		if errSummary == "" {
			errSummary = "unknown tool error"
		}
		dedupeKey := "error|" + call.Name + "|" + toolCallSignature(call) + "|" + errSummary
		if _, exists := state.emittedToolErrors[dedupeKey]; exists {
			continue
		}
		state.emittedToolErrors[dedupeKey] = struct{}{}
		logChat.Warnf(
			"%s [EMIT] tool=%s%s error=%s",
			toolLogPrefixError,
			call.Name,
			summarizeToolTarget(call, "unknown"),
			singleLineToolLog(errSummary),
		)
		outputCh <- llm.StreamChunk{
			ToolStatus: "tool_error",
			ToolName:   call.Name,
			ToolInput:  call.Input,
			ToolError:  errSummary,
		}
	}
}

func (s *ChatService) continueWithToolResults(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	personaID string,
	pendingToolCalls []domain.ToolCall,
	executableCalls []domain.ToolCall,
	guardResults []domain.ToolResult,
	multimodalHistory []domain.MultimodalMessage,
	outputCh chan<- llm.StreamChunk,
	state *toolLoopState,
	round int,
) (<-chan llm.StreamChunk, []domain.MultimodalMessage, bool) {
	toolResults := append([]domain.ToolResult(nil), guardResults...)
	if len(executableCalls) > 0 {
		executed := s.executeTools(ctx, executableCalls, personaID)
		toolResults = append(toolResults, executed...)
		state.executedToolCalls = append(state.executedToolCalls, executableCalls...)
	}

	s.emitToolErrors(outputCh, pendingToolCalls, toolResults, state)
	emitToolStatuses(outputCh, "tool_done", nil)

	toolResultsText := formatToolResults(pendingToolCalls, toolResults)
	state.allToolResultsText += toolResultsText

	conv.AppendAssistantMessage("[Tool calls executed]")
	conv.Messages = append(conv.Messages, domain.Message{
		Role:    domain.RoleUser,
		Content: toolResultsText,
	})

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

	logChat.Logf("continuing with tool results → LLM (round %d)", round+1)
	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		outputCh <- llm.StreamChunk{Error: fmt.Errorf("assemble context after tools: %w", err), Done: true}
		return nil, multimodalHistory, false
	}
	s.logPromptSnapshot("stream-continuation", conv.ChannelID, conv.PersonaID, conv.ID, assembled)

	var nextStream <-chan llm.StreamChunk
	if multimodalHistory != nil {
		state.lastRawLLMInput = formatMultimodalLLMPrompt(assembled.SystemPrompt, multimodalHistory)
		state.lastRawLLMInputHuman = formatMultimodalLLMPromptHuman(assembled.SystemPrompt, multimodalHistory)
		nextStream, err = s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
	} else {
		state.lastRawLLMInput = formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
		state.lastRawLLMInputHuman = formatLLMPromptHuman(assembled.SystemPrompt, assembled.Messages)
		nextStream, err = s.provider.StreamChat(ctx, llm.ChatRequest{
			SystemPrompt: assembled.SystemPrompt,
			Messages:     assembled.Messages,
			Generation:   assembled.Generation,
			ToolMode:     llm.ToolModeNormal,
		})
	}
	if err != nil {
		outputCh <- llm.StreamChunk{Error: fmt.Errorf("llm continuation: %w", err), Done: true}
		return nil, multimodalHistory, false
	}
	return nextStream, multimodalHistory, true
}

func (s *ChatService) finalizeToolLoopResponse(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	outputCh chan<- llm.StreamChunk,
	state *toolLoopState,
) assistantOutputView {
	if strings.TrimSpace(state.fullResponse) == "" && state.hadToolCallAttempt && state.guardSummary.shouldForceFinalAnswer(state.toolPolicy) {
		state.guardSummary.Add(toolGuardKindForcedFinal, "", "tool_guard_forced_final_answer", "")
		forcedResponse, forcedTokens, forcedRawInput, forcedRawInputHuman, err := s.forceFinalAnswerWithoutTools(ctx, conv, persona, state.guardSummary)
		state.totalTokens += forcedTokens
		if strings.TrimSpace(forcedRawInput) != "" {
			state.lastRawLLMInput = forcedRawInput
			state.lastRawLLMInputHuman = forcedRawInputHuman
		}
		forcedFinalReady := false
		if err != nil {
			state.fallbackCause = fmt.Sprintf("forced_final_error=%v", err)
			logChat.Warnf("%s [FORCED_FINAL] failed: %v", toolLogPrefixGuard, err)
		} else if strings.TrimSpace(forcedResponse) == "" {
			state.fallbackCause = "forced_final_empty_output"
			logChat.Warnf("%s [FORCED_FINAL] returned empty output", toolLogPrefixGuard)
		} else {
			candidate := strings.TrimSpace(forcedResponse)
			if shouldRepairForcedFinalOutput(candidate, state.promptPolicy) {
				logChat.Warnf("%s [FORCED_FINAL] output malformed; attempting one repair pass", toolLogPrefixGuard)
				repairedResponse, repairedTokens, repairedRawInput, repairedRawInputHuman, repairErr := s.regenerateForcedFinalOnceWithoutTools(ctx, conv, persona, candidate)
				state.totalTokens += repairedTokens
				if strings.TrimSpace(repairedRawInput) != "" {
					state.lastRawLLMInput = repairedRawInput
					state.lastRawLLMInputHuman = repairedRawInputHuman
				}
				if repairErr != nil {
					state.fallbackCause = fmt.Sprintf("forced_final_repair_error=%v", repairErr)
					logChat.Warnf("%s [FORCED_FINAL] repair failed: %v", toolLogPrefixGuard, repairErr)
				} else if strings.TrimSpace(repairedResponse) == "" {
					state.fallbackCause = "forced_final_repair_empty_output"
					logChat.Warnf("%s [FORCED_FINAL] repair returned empty output", toolLogPrefixGuard)
				} else {
					candidate = strings.TrimSpace(repairedResponse)
					logChat.Logf("%s [FORCED_FINAL] repair pass completed", toolLogPrefixGuard)
				}
			}
			if isForcedFinalOutputValid(candidate, state.promptPolicy) {
				state.fullResponse = candidate
				forcedFinalReady = true
			} else {
				state.fallbackCause = "forced_final_invalid_after_repair"
				logChat.Warnf("%s [FORCED_FINAL] still invalid after repair pass", toolLogPrefixGuard)
			}
		}
		if forcedFinalReady {
			s.incrementToolForcedFinalSuccess()
			logChat.Logf("%s [FORCED_FINAL] generated after %d tool call(s)", toolLogPrefixGuard, len(state.executedToolCalls))
		} else {
			s.incrementToolForcedFinalFailed()
			logChat.Warnf("%s [FORCED_FINAL] unavailable after guard stop; fallback path engaged", toolLogPrefixFallback)
		}
	}

	if strings.TrimSpace(state.fullResponse) == "" && state.hadToolCallAttempt {
		s.incrementToolLoopFallback()
		state.fullResponse = buildToolLoopFallbackResponse(state.promptPolicy, state.guardSummary, state.fallbackCause)
		details := state.guardSummary.buildUserFacingDetails()
		if strings.TrimSpace(details) == "" {
			details = state.fallbackCause
		}
		logChat.Warnf(
			"%s generated after %d tool call(s): %s",
			toolLogPrefixFallback,
			len(state.executedToolCalls),
			singleLineToolLog(strings.TrimSpace(details)),
		)
	}

	finalView := s.prepareAssistantOutputForStorage(state.fullResponse)
	if state.hideAnalysis && finalView.Display != "" {
		outputCh <- llm.StreamChunk{Content: finalView.Display}
		state.finalTextSent = true
	}
	if !state.hideAnalysis && !state.finalTextSent && finalView.Display != "" {
		outputCh <- llm.StreamChunk{Content: finalView.Display}
		state.finalTextSent = true
	}

	return finalView
}

func (s *ChatService) persistToolLoopConversation(
	ctx context.Context,
	conv *domain.Conversation,
	messageToStore string,
	contextForLLM string,
	persona *domain.PersonaModule,
	personaID string,
	imageRefs []domain.ImageRef,
	finalView assistantOutputView,
	outputCh chan<- llm.StreamChunk,
	state *toolLoopState,
) {
	userContent := s.encodeUserMessageForStorage(ctx, conv.ChannelID, personaID, messageToStore, contextForLLM)
	userDisplay := strings.TrimSpace(messageToStore)
	if userDisplay == "" {
		userDisplay = strings.TrimSpace(tooncodec.HumanizeTOON(userContent))
	}
	if userDisplay == "" {
		userDisplay = messageToStore
	}

	assistantText := finalView.Content
	if strings.TrimSpace(assistantText) == "" {
		assistantText = strings.TrimSpace(state.fullResponse)
	}
	assistantContent := s.encodeAssistantMessageForStorage(ctx, conv.ChannelID, persona, personaID, assistantText)

	if len(state.executedToolCalls) > 0 {
		toolUsageAnnotation := formatToolUsageAnnotation(state.executedToolCalls)
		display := finalView.Display
		if strings.TrimSpace(display) == "" {
			display = assistantText
		}
		finalResponseWithAnnotation := display + "\n\n" + toolUsageAnnotation
		outputCh <- llm.StreamChunk{Content: "\n\n" + toolUsageAnnotation}

		rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s", state.allToolResultsText, state.fullResponse)
		if state.baseMessageCount <= len(conv.Messages) {
			conv.Messages = conv.Messages[:state.baseMessageCount]
		}
		if len(imageRefs) > 0 {
			conv.AppendUserMessageWithImages(userContent, userDisplay, imageRefs)
		} else {
			conv.AppendUserMessageWithDisplay(userContent, userDisplay)
		}
		conv.AppendAssistantMessageWithDisplayAndRawViews(
			assistantContent,
			finalResponseWithAnnotation,
			state.lastRawLLMInput,
			state.lastRawLLMInputHuman,
			rawLLMOutput,
		)
		s.applyTokenAccounting(conv, state.totalTokens)
		if err := s.store.Save(ctx, conv); err != nil {
			logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
		}
		return
	}

	if state.fullResponse == "" && messageToStore == "" {
		return
	}
	if state.baseMessageCount <= len(conv.Messages) {
		conv.Messages = conv.Messages[:state.baseMessageCount]
	}
	if len(imageRefs) > 0 {
		conv.AppendUserMessageWithImages(userContent, userDisplay, imageRefs)
	} else {
		conv.AppendUserMessageWithDisplay(userContent, userDisplay)
	}
	conv.AppendAssistantMessageWithDisplayAndRawViews(
		assistantContent,
		finalView.Display,
		state.lastRawLLMInput,
		state.lastRawLLMInputHuman,
		finalView.RawOutput,
	)
	s.applyTokenAccounting(conv, state.totalTokens)
	if err := s.store.Save(ctx, conv); err != nil {
		logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
	}
}
