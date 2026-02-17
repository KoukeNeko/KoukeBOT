package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/tools"
)

func TestHandleMessagePersistsRawLLMInputSnapshots(t *testing.T) {
	ctx := context.Background()
	provider := &rawInputCaptureProvider{
		chatResponse: "[Status]: Stable\n[Analysis]: Test mode.\n[Response]: 已收到。",
	}
	convStore := store.NewMemoryStore()
	service := NewChatService(
		provider,
		NewContextAssembler(),
		convStore,
		nil,
		0,
		map[string]*domain.PersonaModule{
			"persona-a": rawInputTestPersona("persona-a"),
		},
		nil,
		nil,
	)

	_, err := service.HandleMessage(ctx, "ch-raw", "persona-a", "hello")
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if len(provider.chatRequests) != 1 {
		t.Fatalf("chat requests = %d, want 1", len(provider.chatRequests))
	}

	conv, err := convStore.Get(ctx, "ch-raw", "persona-a")
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if len(conv.Messages) < 2 {
		t.Fatalf("messages len = %d, want >= 2", len(conv.Messages))
	}
	last := conv.Messages[len(conv.Messages)-1]
	if last.Role != domain.RoleAssistant {
		t.Fatalf("last role = %s, want assistant", last.Role)
	}

	expectedRaw := formatLLMPrompt(provider.chatRequests[0].SystemPrompt, provider.chatRequests[0].Messages)
	expectedRawHuman := formatLLMPromptHuman(provider.chatRequests[0].SystemPrompt, provider.chatRequests[0].Messages)
	if last.RawLLMInput != expectedRaw {
		t.Fatalf("raw_llm_input mismatch")
	}
	if last.RawLLMInputHuman != expectedRawHuman {
		t.Fatalf("raw_llm_input_human mismatch")
	}
}

func TestHandleMessageStreamPersistsLastRoundRawLLMInput(t *testing.T) {
	ctx := context.Background()
	provider := &rawInputCaptureProvider{
		streamResponses: [][]llm.StreamChunk{
			{
				{
					ToolCalls: []domain.ToolCall{
						{
							ID:    "tool-1",
							Name:  "raw_input_echo",
							Input: map[string]interface{}{"value": "x"},
						},
					},
				},
			},
			{
				{
					Content:    "[Status]: Stable\n[Analysis]: Continued with tool result.\n[Response]: 完成。",
					TokensUsed: 11,
				},
				{Done: true},
			},
		},
	}
	convStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	service := NewChatService(
		provider,
		NewContextAssembler(),
		convStore,
		nil,
		0,
		map[string]*domain.PersonaModule{
			"persona-a": rawInputTestPersona("persona-a"),
		},
		[]domain.Tool{&rawInputEchoTool{}},
		nil,
	)

	streamCh, err := service.HandleMessageStream(ctx, "ch-stream", "persona-a", "hello stream")
	if err != nil {
		t.Fatalf("HandleMessageStream() error = %v", err)
	}
	var output strings.Builder
	for chunk := range streamCh {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		output.WriteString(chunk.Content)
	}
	if !strings.Contains(output.String(), "完成") {
		t.Fatalf("expected streamed final response, got: %s", output.String())
	}
	if len(provider.streamRequests) < 2 {
		t.Fatalf("stream requests = %d, want >= 2", len(provider.streamRequests))
	}

	conv, err := convStore.Get(ctx, "ch-stream", "persona-a")
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if len(conv.Messages) == 0 {
		t.Fatalf("messages len = 0, want > 0")
	}
	last := conv.Messages[len(conv.Messages)-1]
	if last.Role != domain.RoleAssistant {
		t.Fatalf("last role = %s, want assistant", last.Role)
	}

	lastReq := provider.streamRequests[len(provider.streamRequests)-1]
	expectedRaw := formatLLMPrompt(lastReq.SystemPrompt, lastReq.Messages)
	expectedRawHuman := formatLLMPromptHuman(lastReq.SystemPrompt, lastReq.Messages)
	if last.RawLLMInput != expectedRaw {
		t.Fatalf("raw_llm_input should use last tool-loop round prompt")
	}
	if last.RawLLMInputHuman != expectedRawHuman {
		t.Fatalf("raw_llm_input_human should use last tool-loop round prompt")
	}
}

func TestHandleMessageStreamBreaksEarlyWhenAllToolCallsBlocked(t *testing.T) {
	ctx := context.Background()
	provider := &loopingToolCallProvider{}
	convStore := store.NewMemoryStore()
	service := NewChatService(
		provider,
		NewContextAssembler(),
		convStore,
		nil,
		0,
		map[string]*domain.PersonaModule{
			"persona-a": rawInputTestPersona("persona-a"),
		},
		[]domain.Tool{tools.NewSessionStatusTool("Asia/Taipei", "24")},
		nil,
	)

	policy := DefaultToolExecutionPolicy()
	policy.ToolGuardEnabled = false
	policy.MaxToolRounds = 2
	policy.ForceFinalAnswerAfterToolLimit = true
	service.UpdateToolExecutionPolicy(policy)

	streamCh, err := service.HandleMessageStream(ctx, "ch-loop", "persona-a", "hello loop")
	if err != nil {
		t.Fatalf("HandleMessageStream() error = %v", err)
	}

	var output strings.Builder
	for chunk := range streamCh {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		output.WriteString(chunk.Content)
	}

	finalText := output.String()
	if !strings.Contains(finalText, "forced final") {
		t.Fatalf("expected forced-final content after max rounds, got: %s", finalText)
	}
	if provider.disabledCalls != 1 {
		t.Fatalf("disabled StreamChat calls = %d, want 1", provider.disabledCalls)
	}
	if provider.normalCalls != 2 {
		t.Fatalf("normal StreamChat calls = %d, want 2 (initial + 1 continuation before all-blocked early break)", provider.normalCalls)
	}
}

type rawInputCaptureProvider struct {
	chatResponse    string
	chatRequests    []llm.ChatRequest
	streamResponses [][]llm.StreamChunk
	streamRequests  []llm.ChatRequest
}

type loopingToolCallProvider struct {
	normalCalls   int
	disabledCalls int
}

func (p *rawInputCaptureProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	p.chatRequests = append(p.chatRequests, req)
	response := p.chatResponse
	if strings.TrimSpace(response) == "" {
		response = "ok"
	}
	return &llm.ChatResponse{
		Content:    response,
		TokensUsed: 17,
	}, nil
}

func (p *rawInputCaptureProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.streamRequests = append(p.streamRequests, req)
	idx := len(p.streamRequests) - 1
	var chunks []llm.StreamChunk
	if idx < len(p.streamResponses) {
		chunks = p.streamResponses[idx]
	}
	ch := make(chan llm.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *rawInputCaptureProvider) StreamChatMultimodal(_ context.Context, _ string, _ []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *rawInputCaptureProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking:  true,
		ContextWindow:     1_000_000,
		SupportsStreaming: true,
		ProviderName:      "raw-input-test",
	}
}

func (p *rawInputCaptureProvider) RegisterTools(_ []domain.Tool) {}

func (p *loopingToolCallProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content:    "unused",
		TokensUsed: 1,
	}, nil
}

func (p *loopingToolCallProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 2)
	if req.ToolMode == llm.ToolModeDisabled {
		p.disabledCalls++
		ch <- llm.StreamChunk{
			Content: "[Status]: Stable\n[Analysis]: Tool loop capped.\n[Response]: forced final",
		}
		ch <- llm.StreamChunk{Done: true}
		close(ch)
		return ch, nil
	}

	p.normalCalls++
	ch <- llm.StreamChunk{
		ToolCalls: []domain.ToolCall{
			{
				ID:    "loop-call",
				Name:  "session_status",
				Input: map[string]interface{}{},
			},
		},
	}
	close(ch)
	return ch, nil
}

func (p *loopingToolCallProvider) StreamChatMultimodal(_ context.Context, _ string, _ []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *loopingToolCallProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking:  true,
		ContextWindow:     1_000_000,
		SupportsStreaming: true,
		ProviderName:      "looping-tool-call-test",
	}
}

func (p *loopingToolCallProvider) RegisterTools(_ []domain.Tool) {}

type rawInputEchoTool struct{}

func (t *rawInputEchoTool) Name() string { return "raw_input_echo" }

func (t *rawInputEchoTool) Description() string { return "echo test tool" }

func (t *rawInputEchoTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}

func (t *rawInputEchoTool) Execute(_ context.Context, _ map[string]interface{}) (domain.ToolResult, error) {
	return domain.ToolResult{Content: "echo-ok"}, nil
}

func rawInputTestPersona(id string) *domain.PersonaModule {
	return &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta: domain.PersonaMeta{
				ID:   id,
				Name: "Raw Input Persona",
			},
			SystemTemplate: "You are test persona.\n{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
		},
		Lore: "raw input persona lore",
	}
}
