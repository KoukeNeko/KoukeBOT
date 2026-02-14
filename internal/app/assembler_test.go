package app

import (
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestAssembleThinkingModelIncludesRelevantDigestAndRecentMessage(t *testing.T) {
	assembler := NewContextAssembler()
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "You are a test assistant.\n{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
		},
		Lore: "persona lore",
	}

	conv := &domain.Conversation{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Earlier conversation about setup", Timestamp: time.Now().Add(-2 * time.Hour)},
			{Role: domain.RoleAssistant, Content: "Acknowledged", Timestamp: time.Now().Add(-2 * time.Hour)},
			{Role: domain.RoleUser, Content: "Can you check phoenix project status?", Timestamp: time.Now()},
		},
		ContextDigests: []domain.ConversationDigest{
			{
				ID:        "digest-1",
				Summary:   "User previously decided to use Project Phoenix migration plan.",
				Keywords:  []string{"phoenix", "migration", "project"},
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}

	assembled, err := assembler.Assemble(persona, conv, llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    12000,
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if len(assembled.Messages) == 0 {
		t.Fatalf("assembled messages should not be empty")
	}
	if !strings.Contains(assembled.Messages[0].Content, "Relevant conversation digests") {
		t.Fatalf("first message should include digest block")
	}
	last := assembled.Messages[len(assembled.Messages)-1]
	if last.Role != domain.RoleUser || !strings.Contains(last.Content, "phoenix project status") {
		t.Fatalf("latest recent message should be preserved, got role=%s content=%q", last.Role, last.Content)
	}
}

func TestAssembleThinkingModelInjectsPersonaLockEveryNTurns(t *testing.T) {
	assembler := NewContextAssembler()
	assembler.budget.PersonaLockEveryTurns = 2

	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           domain.PersonaMeta{Name: "Satsuki"},
			SystemTemplate: "You are {{.Lore}}\n{{.Anchors}}\n{{.Time}}",
		},
		Lore: "a persona",
	}

	conv := &domain.Conversation{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "hi", Timestamp: time.Now().Add(-2 * time.Minute)},
			{Role: domain.RoleAssistant, Content: "yo", Timestamp: time.Now().Add(-90 * time.Second)},
			{Role: domain.RoleUser, Content: "second user turn", Timestamp: time.Now()},
		},
	}

	assembled, err := assembler.Assemble(persona, conv, llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    8000,
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}

	found := false
	for _, msg := range assembled.Messages {
		if strings.Contains(msg.Content, "[Persona lock]") && strings.Contains(msg.Content, "Satsuki") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected persona lock message to be injected")
	}
}
