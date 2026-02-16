package llm

import (
	"context"
	"strings"
	"testing"
)

func TestSessionStatusToolOutputIncludesNoRepeatInstruction(t *testing.T) {
	tool := NewSessionStatusTool("Asia/Taipei", "24")
	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() should not return error result: %+v", result)
	}

	wants := []string{
		`"timezone": "Asia/Taipei"`,
		`"unix_timestamp":`,
		"do NOT recompute timezone from `unix_timestamp`",
		"do NOT call `session_status` again in this turn",
	}
	for _, want := range wants {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("result should contain %q, got:\n%s", want, result.Content)
		}
	}
}
