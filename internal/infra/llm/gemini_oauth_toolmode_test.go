package llm

import (
	"context"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestBuildRequestToolModeDisabledOmitsTools(t *testing.T) {
	provider := &GeminiOAuthProvider{
		model:     "gemini-test",
		projectID: "proj-test",
		tools: []domain.Tool{
			toolModeTestTool{},
		},
	}

	req := ChatRequest{
		SystemPrompt: "system",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "hello"},
		},
		Generation: domain.GenerationConfig{
			Temperature: 0.7,
			MaxTokens:   256,
			TopP:        0.9,
		},
		ToolMode: ToolModeDisabled,
	}

	caReq := provider.buildRequest(req)
	if len(caReq.Request.Tools) != 0 {
		t.Fatalf("expected no tools when tool_mode=disabled, got %d", len(caReq.Request.Tools))
	}
}

func TestBuildRequestToolModeNormalKeepsTools(t *testing.T) {
	provider := &GeminiOAuthProvider{
		model:     "gemini-test",
		projectID: "proj-test",
		tools: []domain.Tool{
			toolModeTestTool{},
		},
	}

	req := ChatRequest{
		SystemPrompt: "system",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "hello"},
		},
		Generation: domain.GenerationConfig{
			Temperature: 0.7,
			MaxTokens:   256,
			TopP:        0.9,
		},
		ToolMode: ToolModeNormal,
	}

	caReq := provider.buildRequest(req)
	if len(caReq.Request.Tools) == 0 {
		t.Fatalf("expected tools when tool_mode=normal")
	}
}

type toolModeTestTool struct{}

func (toolModeTestTool) Name() string { return "tool_mode_test" }

func (toolModeTestTool) Description() string { return "tool mode test tool" }

func (toolModeTestTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}

func (toolModeTestTool) Execute(_ context.Context, _ map[string]interface{}) (domain.ToolResult, error) {
	return domain.ToolResult{Content: "ok"}, nil
}
