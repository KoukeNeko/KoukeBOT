package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestExecuteToolsFileWriteUsesRuntimePersona(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWriteFile(t, filepath.Join(personaConfigsBaseDir, "start", "user.md"), "start\n")
	mustWriteFile(t, filepath.Join(personaConfigsBaseDir, "runtime", "user.md"), "runtime\n")

	// Simulate startup declarations being bound to a different persona.
	service := NewChatService(nil, nil, nil, nil, 0, nil, []domain.Tool{
		llm.NewFileWriteTool(filepath.Join(personaConfigsBaseDir, "start")),
	}, nil)

	results := service.executeTools(context.Background(), []domain.ToolCall{
		{
			ID:   "1",
			Name: "file_write",
			Input: map[string]interface{}{
				"filename": "user.md",
				"content":  "written-by-runtime",
			},
		},
	}, "runtime")

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].IsError {
		t.Fatalf("tool should succeed, got error: %s", results[0].Content)
	}

	startContent := mustReadFile(t, filepath.Join(personaConfigsBaseDir, "start", "user.md"))
	if strings.Contains(startContent, "written-by-runtime") {
		t.Fatalf("start persona file should remain unchanged, got: %q", startContent)
	}

	runtimeContent := mustReadFile(t, filepath.Join(personaConfigsBaseDir, "runtime", "user.md"))
	if !strings.Contains(runtimeContent, "written-by-runtime") {
		t.Fatalf("runtime persona file was not updated, got: %q", runtimeContent)
	}
}

func TestExecuteToolsFileWriteUsesPersonaDirNameMapping(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWriteFile(t, filepath.Join(personaConfigsBaseDir, "satsuki", "user.md"), "before\n")

	service := NewChatService(nil, nil, nil, nil, 0, map[string]*domain.PersonaModule{
		"koto_satsuki_watanare": {
			DirName: "satsuki",
		},
	}, nil, nil)

	results := service.executeTools(context.Background(), []domain.ToolCall{
		{
			ID:   "1",
			Name: "file_write",
			Input: map[string]interface{}{
				"filename": "user.md",
				"content":  "persona-id-mapped-write",
			},
		},
	}, "koto_satsuki_watanare")

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].IsError {
		t.Fatalf("tool should succeed, got error: %s", results[0].Content)
	}

	content := mustReadFile(t, filepath.Join(personaConfigsBaseDir, "satsuki", "user.md"))
	if !strings.Contains(content, "persona-id-mapped-write") {
		t.Fatalf("expected mapped directory write, got: %q", content)
	}
}

func TestExecuteToolsFileWriteReturnsErrorWhenPersonaMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	service := NewChatService(nil, nil, nil, nil, 0, nil, nil, nil)

	results := service.executeTools(context.Background(), []domain.ToolCall{
		{
			ID:   "1",
			Name: "file_write",
			Input: map[string]interface{}{
				"filename": "user.md",
				"content":  "x",
			},
		},
	}, "missing")

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if !results[0].IsError {
		t.Fatalf("expected error when persona directory is missing")
	}
	if !strings.Contains(results[0].Content, `persona "missing" directory not found`) {
		t.Fatalf("unexpected error: %s", results[0].Content)
	}
}

func TestExecuteToolsNonFileToolUsesRegistry(t *testing.T) {
	service := NewChatService(nil, nil, nil, nil, 0, nil, []domain.Tool{&fakeTool{}}, nil)

	results := service.executeTools(context.Background(), []domain.ToolCall{
		{
			ID:   "1",
			Name: "fake_tool",
			Input: map[string]interface{}{
				"value": "ok",
			},
		},
	}, "any-persona")

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].IsError {
		t.Fatalf("expected non-file tool to succeed, got: %s", results[0].Content)
	}
	if results[0].Content != "fake-success" {
		t.Fatalf("unexpected non-file tool output: %s", results[0].Content)
	}
}

func TestToolCallSignatureStableForMapInput(t *testing.T) {
	callA := domain.ToolCall{
		Name: "session_status",
		Input: map[string]interface{}{
			"timezone": "Asia/Taipei",
			"format":   "24",
		},
	}
	callB := domain.ToolCall{
		Name: "session_status",
		Input: map[string]interface{}{
			"format":   "24",
			"timezone": "Asia/Taipei",
		},
	}

	sigA := toolCallSignature(callA)
	sigB := toolCallSignature(callB)
	if sigA != sigB {
		t.Fatalf("expected stable signature, got %q != %q", sigA, sigB)
	}
}

func TestBuildToolLoopFallbackResponse(t *testing.T) {
	policy := DefaultPromptGuardPolicy()
	policy.TripletOutputEnabled = true
	triplet := buildToolLoopFallbackResponse(policy)
	if !strings.Contains(triplet, "[Status]:") || !strings.Contains(triplet, "[Response]:") {
		t.Fatalf("expected triplet fallback response, got: %s", triplet)
	}

	policy.TripletOutputEnabled = false
	plain := buildToolLoopFallbackResponse(policy)
	if strings.Contains(plain, "[Status]:") {
		t.Fatalf("expected plain fallback response, got: %s", plain)
	}
}

func TestNormalizeToolExecutionPolicyBounds(t *testing.T) {
	p := normalizeToolExecutionPolicy(ToolExecutionPolicy{
		MaxToolRounds:                  99,
		MaxWebSearchCallsPerTurn:       0,
		ForceFinalAnswerAfterToolLimit: true,
	})
	if p.MaxToolRounds != 6 {
		t.Fatalf("MaxToolRounds = %d, want 6", p.MaxToolRounds)
	}
	if p.MaxWebSearchCallsPerTurn != DefaultMaxWebSearchCallsPerTurn {
		t.Fatalf("MaxWebSearchCallsPerTurn = %d, want %d", p.MaxWebSearchCallsPerTurn, DefaultMaxWebSearchCallsPerTurn)
	}
}

func TestToolGuardSummaryForceFinalAnswer(t *testing.T) {
	policy := DefaultToolExecutionPolicy()
	summary := toolGuardSummary{}
	if summary.shouldForceFinalAnswer(policy) {
		t.Fatalf("empty summary should not trigger forced final answer")
	}

	summary.Add(toolGuardKindDuplicate, "web_search", "duplicate_tool_call_blocked", "")
	if !summary.shouldForceFinalAnswer(policy) {
		t.Fatalf("duplicate guard should trigger forced final answer")
	}

	policy.ForceFinalAnswerAfterToolLimit = false
	if summary.shouldForceFinalAnswer(policy) {
		t.Fatalf("policy disabled should suppress forced final answer")
	}
}

type fakeTool struct{}

func (f *fakeTool) Name() string { return "fake_tool" }
func (f *fakeTool) Description() string {
	return "test-only fake tool"
}
func (f *fakeTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}
func (f *fakeTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	return domain.ToolResult{
		Content: "fake-success",
	}, nil
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(b)
}
