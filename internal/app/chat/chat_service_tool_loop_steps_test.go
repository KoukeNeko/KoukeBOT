package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestConsumeRoundStreamCollectsVisibleContentAndToolCalls(t *testing.T) {
	service := &ChatService{}
	state := newToolLoopState(DefaultToolExecutionPolicy(), PromptGuardPolicy{})
	outputCh := make(chan llm.StreamChunk, 4)
	streamCh := make(chan llm.StreamChunk, 4)

	streamCh <- llm.StreamChunk{Content: "hello", TokensUsed: 5}
	streamCh <- llm.StreamChunk{
		ToolCalls: []domain.ToolCall{
			{ID: "1", Name: "web_search", Input: map[string]interface{}{"query": "q"}},
		},
	}
	close(streamCh)

	pending, visible, ok := service.consumeRoundStream(streamCh, outputCh, state)
	if !ok {
		t.Fatalf("consumeRoundStream returned not ok")
	}
	if len(pending) != 1 {
		t.Fatalf("pending tool calls = %d, want 1", len(pending))
	}
	if visible != "hello" {
		t.Fatalf("visible content = %q, want hello", visible)
	}
	if state.fullResponse != "hello" {
		t.Fatalf("full response = %q, want hello", state.fullResponse)
	}
	if state.totalTokens != 5 {
		t.Fatalf("total tokens = %d, want 5", state.totalTokens)
	}
}

func TestFilterExecutableToolCallsBlocksDuplicate(t *testing.T) {
	service := &ChatService{}
	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = true
	policy.MaxWebSearchCallsPerTurn = 3
	state := newToolLoopState(policy, PromptGuardPolicy{})

	calls := []domain.ToolCall{
		{ID: "1", Name: "web_search", Input: map[string]interface{}{"query": "same"}},
		{ID: "2", Name: "web_search", Input: map[string]interface{}{"query": "same"}},
	}

	executable, guards := service.filterExecutableToolCalls(calls, "persona", 0, state)
	if len(executable) != 1 {
		t.Fatalf("executable calls = %d, want 1", len(executable))
	}
	if len(guards) != 1 {
		t.Fatalf("guard results = %d, want 1", len(guards))
	}
	if !state.guardSummary.Has(toolGuardKindDuplicate) {
		t.Fatalf("expected duplicate guard event")
	}
}

func TestFilterExecutableToolCallsBlocksLimit(t *testing.T) {
	service := &ChatService{}
	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = true
	policy.MaxWebSearchCallsPerTurn = 1
	state := newToolLoopState(policy, PromptGuardPolicy{})

	calls := []domain.ToolCall{
		{ID: "1", Name: "web_search", Input: map[string]interface{}{"query": "first"}},
		{ID: "2", Name: "web_search", Input: map[string]interface{}{"query": "second"}},
	}

	executable, guards := service.filterExecutableToolCalls(calls, "persona", 0, state)
	if len(executable) != 1 {
		t.Fatalf("executable calls = %d, want 1", len(executable))
	}
	if len(guards) != 1 {
		t.Fatalf("guard results = %d, want 1", len(guards))
	}
	if !state.guardSummary.Has(toolGuardKindLimit) {
		t.Fatalf("expected limit guard event")
	}
}

func TestFilterExecutableToolCallsSkipsGuardsWhenDisabled(t *testing.T) {
	service := &ChatService{}
	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = false
	policy.MaxWebSearchCallsPerTurn = 1
	state := newToolLoopState(policy, PromptGuardPolicy{})

	calls := []domain.ToolCall{
		{ID: "1", Name: "web_search", Input: map[string]interface{}{"query": "same"}},
		{ID: "2", Name: "web_search", Input: map[string]interface{}{"query": "same"}},
	}

	executable, guards := service.filterExecutableToolCalls(calls, "persona", 0, state)
	if len(executable) != 2 {
		t.Fatalf("executable calls = %d, want 2", len(executable))
	}
	if len(guards) != 0 {
		t.Fatalf("guard results = %d, want 0", len(guards))
	}
	if !state.guardSummary.Empty() {
		t.Fatalf("expected no guard events when tool guard is disabled")
	}
}

func TestFinalizeToolLoopResponseBuildsFallback(t *testing.T) {
	service := &ChatService{
		assembler: NewContextAssembler(),
	}
	state := newToolLoopState(DefaultToolExecutionPolicy(), PromptGuardPolicy{})
	state.hadToolCallAttempt = true
	state.fallbackCause = "forced_final_error=network timeout"
	state.fullResponse = ""
	outputCh := make(chan llm.StreamChunk, 2)

	view := service.finalizeToolLoopResponse(context.Background(), &domain.Conversation{}, nil, outputCh, state)
	if view.Display == "" {
		t.Fatalf("expected non-empty fallback display")
	}
	if state.fullResponse == "" {
		t.Fatalf("expected fallback full response to be set")
	}
	if len(outputCh) == 0 {
		t.Fatalf("expected fallback chunk to be emitted")
	}
	chunk := <-outputCh
	if chunk.Content == "" {
		t.Fatalf("expected non-empty chunk content")
	}
	if !strings.Contains(chunk.Content, "forced_final_error=network timeout") {
		t.Fatalf("expected fallback to include full cause, got: %s", chunk.Content)
	}
}

func TestEmitToolErrorsDoesNotTruncateContent(t *testing.T) {
	service := &ChatService{}
	policy := DefaultToolExecutionPolicy()
	policy.DiscordShowInternalToolErrors = true
	state := newToolLoopState(policy, PromptGuardPolicy{})
	outputCh := make(chan llm.StreamChunk, 1)

	pending := []domain.ToolCall{
		{ID: "1", Name: "file_read", Input: map[string]interface{}{"filename": "MEMORY.md"}},
	}
	fullErr := strings.Repeat("x", 500)
	results := []domain.ToolResult{
		{
			ToolCallID: "1",
			Content:    fullErr,
			IsError:    true,
		},
	}

	service.emitToolErrors(outputCh, pending, results, state)
	if len(outputCh) != 1 {
		t.Fatalf("expected one tool error chunk, got %d", len(outputCh))
	}
	chunk := <-outputCh
	if chunk.ToolError != fullErr {
		t.Fatalf("tool error was truncated: got_len=%d want_len=%d", len(chunk.ToolError), len(fullErr))
	}
}

func TestEmitToolErrorsEmitsToolGuardStatus(t *testing.T) {
	service := &ChatService{}
	policy := DefaultToolExecutionPolicy()
	policy.DiscordShowInternalToolErrors = true
	state := newToolLoopState(policy, PromptGuardPolicy{})
	outputCh := make(chan llm.StreamChunk, 1)

	pending := []domain.ToolCall{
		{ID: "1", Name: "web_search", Input: map[string]interface{}{"query": "same"}},
	}
	guardContent := `Tool guard: duplicate call blocked for "web_search" with identical input in this turn.`
	results := []domain.ToolResult{
		{
			ToolCallID: "1",
			Content:    guardContent,
			IsError:    true,
		},
	}

	service.emitToolErrors(outputCh, pending, results, state)
	if len(outputCh) != 1 {
		t.Fatalf("expected one tool guard chunk, got %d", len(outputCh))
	}
	chunk := <-outputCh
	if chunk.ToolStatus != "tool_guard" {
		t.Fatalf("tool status = %q, want tool_guard", chunk.ToolStatus)
	}
	if chunk.ToolError != guardContent {
		t.Fatalf("tool guard detail mismatch")
	}
}

func TestFinalizeToolLoopResponseRepairsMalformedForcedFinalOnce(t *testing.T) {
	provider := &forcedFinalTestProvider{
		streamResponses: [][]llm.StreamChunk{
			{
				{
					Content:    "[Status]: Analytical\n[Analysis]: Missing response tag.\nThis is malformed output.",
					TokensUsed: 11,
				},
				{Done: true},
			},
			{
				{
					Content:    "[Status]: Analytical\n[Analysis]: Rewritten into valid output.\n[Response]: 已修復完成。",
					TokensUsed: 7,
				},
				{Done: true},
			},
		},
	}
	assembler := NewContextAssembler()
	service := &ChatService{
		assembler: assembler,
		provider:  provider,
	}
	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = true
	state := newToolLoopState(policy, assembler.PromptGuardPolicy())
	state.hadToolCallAttempt = true
	state.guardSummary.Add(toolGuardKindMaxRounds, "", "max_tool_rounds_reached", "max_tool_rounds=3")
	outputCh := make(chan llm.StreamChunk, 2)
	conv := &domain.Conversation{
		ID:        "conv-1",
		ChannelID: "ch",
		PersonaID: "persona-a",
	}

	view := service.finalizeToolLoopResponse(context.Background(), conv, forcedFinalTestPersona("persona-a"), outputCh, state)
	if provider.streamCallCount != 2 {
		t.Fatalf("stream call count = %d, want 2", provider.streamCallCount)
	}
	if len(provider.streamRequests) != 2 {
		t.Fatalf("stream requests = %d, want 2", len(provider.streamRequests))
	}
	if !view.Parsed {
		t.Fatalf("expected repaired output to parse as triplet")
	}
	if view.Content != "已修復完成。" {
		t.Fatalf("view content = %q, want repaired response", view.Content)
	}
	if !strings.Contains(state.fullResponse, "[Response]: 已修復完成。") {
		t.Fatalf("full response should contain repaired triplet, got: %s", state.fullResponse)
	}
	if len(outputCh) == 0 {
		t.Fatalf("expected repaired final chunk to be emitted")
	}
	chunk := <-outputCh
	if !strings.Contains(chunk.Content, "[Response]: 已修復完成。") {
		t.Fatalf("chunk content should include repaired response, got: %s", chunk.Content)
	}
	lastReq := provider.streamRequests[len(provider.streamRequests)-1]
	if state.lastRawLLMInput != formatLLMPrompt(lastReq.SystemPrompt, lastReq.Messages) {
		t.Fatalf("last raw input should match final repair prompt snapshot")
	}
	if state.lastRawLLMInputHuman != formatLLMPromptHuman(lastReq.SystemPrompt, lastReq.Messages) {
		t.Fatalf("last human raw input should match final repair prompt snapshot")
	}
}

func TestFinalizeToolLoopResponseFallsBackWhenRepairFails(t *testing.T) {
	provider := &forcedFinalTestProvider{
		streamResponses: [][]llm.StreamChunk{
			{
				{
					Content:    "[Status]: Analytical\n[Analysis]: Missing response tag.\nMalformed.",
					TokensUsed: 5,
				},
				{Done: true},
			},
			{
				{
					Error: errors.New("repair stream failed"),
				},
			},
		},
	}
	assembler := NewContextAssembler()
	service := &ChatService{
		assembler: assembler,
		provider:  provider,
	}
	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = true
	state := newToolLoopState(policy, assembler.PromptGuardPolicy())
	state.hadToolCallAttempt = true
	state.guardSummary.Add(toolGuardKindMaxRounds, "", "max_tool_rounds_reached", "max_tool_rounds=3")
	outputCh := make(chan llm.StreamChunk, 2)
	conv := &domain.Conversation{
		ID:        "conv-2",
		ChannelID: "ch",
		PersonaID: "persona-b",
	}

	view := service.finalizeToolLoopResponse(context.Background(), conv, forcedFinalTestPersona("persona-b"), outputCh, state)
	if provider.streamCallCount != 2 {
		t.Fatalf("stream call count = %d, want 2", provider.streamCallCount)
	}
	if len(provider.streamRequests) != 2 {
		t.Fatalf("stream requests = %d, want 2", len(provider.streamRequests))
	}
	if !strings.Contains(view.Display, "Tool Loop Stopped") {
		t.Fatalf("expected fallback display, got: %s", view.Display)
	}
	if !strings.Contains(state.fullResponse, "Tool Loop Stopped") {
		t.Fatalf("expected fallback full response, got: %s", state.fullResponse)
	}
	if len(outputCh) == 0 {
		t.Fatalf("expected fallback chunk to be emitted")
	}
	chunk := <-outputCh
	if !strings.Contains(chunk.Content, "Tool Loop Stopped") {
		t.Fatalf("expected fallback chunk content, got: %s", chunk.Content)
	}
	lastReq := provider.streamRequests[len(provider.streamRequests)-1]
	if state.lastRawLLMInput != formatLLMPrompt(lastReq.SystemPrompt, lastReq.Messages) {
		t.Fatalf("last raw input should match failed repair prompt snapshot")
	}
	if state.lastRawLLMInputHuman != formatLLMPromptHuman(lastReq.SystemPrompt, lastReq.Messages) {
		t.Fatalf("last human raw input should match failed repair prompt snapshot")
	}
}

type forcedFinalTestProvider struct {
	streamResponses [][]llm.StreamChunk
	streamCallCount int
	streamRequests  []llm.ChatRequest
}

func (p *forcedFinalTestProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *forcedFinalTestProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.streamCallCount >= len(p.streamResponses) {
		return nil, fmt.Errorf("unexpected StreamChat call #%d", p.streamCallCount+1)
	}
	p.streamRequests = append(p.streamRequests, req)
	chunks := p.streamResponses[p.streamCallCount]
	p.streamCallCount++
	ch := make(chan llm.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *forcedFinalTestProvider) StreamChatMultimodal(_ context.Context, _ string, _ []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("unexpected StreamChatMultimodal call")
}

func (p *forcedFinalTestProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking:  true,
		ContextWindow:     1_000_000,
		SupportsStreaming: true,
		ProviderName:      "forced-final-test",
	}
}

func (p *forcedFinalTestProvider) RegisterTools(_ []domain.Tool) {}

func forcedFinalTestPersona(id string) *domain.PersonaModule {
	return &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           domain.PersonaMeta{ID: id, Name: "Test Persona"},
			SystemTemplate: "You are test persona.\n{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
		},
		Lore: "test lore",
	}
}
