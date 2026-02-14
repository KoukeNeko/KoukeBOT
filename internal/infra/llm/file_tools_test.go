package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeManagedMemoryPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "MEMORY.md", want: "memory/MEMORY.md"},
		{in: "memory/memory.md", want: "memory/MEMORY.md"},
		{in: "./MEMORY.md", want: "memory/MEMORY.md"},
		{in: "user.md", want: "user.md"},
		{in: "memory/2026-02-14.md", want: "memory/2026-02-14.md"},
	}

	for _, tt := range tests {
		got, _ := normalizeManagedMemoryPath(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeManagedMemoryPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFileWriteToolRedirectsRootMemoryFile(t *testing.T) {
	personaDir := t.TempDir()
	tool := NewFileWriteTool(personaDir)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"filename": "MEMORY.md",
		"content":  "long term memory",
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool result error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "請改用 memory/MEMORY.md") {
		t.Fatalf("expected path notice in tool result, got: %s", result.Content)
	}

	target := filepath.Join(personaDir, "memory", "MEMORY.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected redirected file at %s: %v", target, err)
	}
}
