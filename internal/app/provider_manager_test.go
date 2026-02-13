package app

import (
	"context"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

type stubTool struct {
	name string
}

func (t stubTool) Name() string {
	return t.name
}

func (t stubTool) Description() string {
	return "stub"
}

func (t stubTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (t stubTool) Execute(context.Context, map[string]interface{}) (domain.ToolResult, error) {
	return domain.ToolResult{Content: "ok"}, nil
}

func TestProviderManagerUpdateReregistersTools(t *testing.T) {
	initial := &stubProvider{id: "initial"}
	next := &stubProvider{id: "next"}
	pm := NewProviderManager(initial)

	pm.RegisterTools([]domain.Tool{stubTool{name: "web_search"}})
	if len(initial.tools) != 1 {
		t.Fatalf("initial provider tools = %d, want 1", len(initial.tools))
	}

	pm.Update(next)
	if len(next.tools) != 1 {
		t.Fatalf("next provider tools = %d, want 1", len(next.tools))
	}
	if next.tools[0].Name() != "web_search" {
		t.Fatalf("next provider first tool = %s, want web_search", next.tools[0].Name())
	}
}

func TestProviderManagerCachesToolsBeforeProviderUpdate(t *testing.T) {
	pm := NewProviderManager(nil)
	pm.RegisterTools([]domain.Tool{stubTool{name: "file_read"}})

	next := &stubProvider{id: "next"}
	pm.Update(next)
	if len(next.tools) != 1 {
		t.Fatalf("next provider tools = %d, want 1", len(next.tools))
	}
	if next.tools[0].Name() != "file_read" {
		t.Fatalf("next provider first tool = %s, want file_read", next.tools[0].Name())
	}
}
