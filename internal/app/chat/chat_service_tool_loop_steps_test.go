package chat

import (
	"context"
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

func TestFinalizeToolLoopResponseBuildsFallback(t *testing.T) {
	service := &ChatService{
		assembler: NewContextAssembler(),
	}
	state := newToolLoopState(DefaultToolExecutionPolicy(), PromptGuardPolicy{})
	state.hadToolCallAttempt = true
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
}
