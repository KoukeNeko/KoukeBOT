package app

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
