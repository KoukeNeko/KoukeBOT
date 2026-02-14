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
				ID:            "digest-1",
				Summary:       "User previously decided to use Project Phoenix migration plan.",
				Keywords:      []string{"phoenix", "migration", "project"},
				Attitude:      "critical",
				EmotionalTone: "cold",
				StyleMarkers:  []string{"sarcastic", "directive"},
				StyleQuotes:   []string{"Please fix your broken logic before asking again."},
				CreatedAt:     time.Now().Add(-1 * time.Hour),
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
	if !strings.Contains(assembled.Messages[0].Content, "Style Contract") {
		t.Fatalf("digest block should include style contract")
	}
	if !strings.Contains(assembled.Messages[0].Content, "Do NOT flatten persona tone") {
		t.Fatalf("digest block should include anti-flattening directive")
	}
	last := assembled.Messages[len(assembled.Messages)-1]
	if last.Role != domain.RoleUser || !strings.Contains(last.Content, "phoenix project status") {
		t.Fatalf("latest recent message should be preserved, got role=%s content=%q", last.Role, last.Content)
	}
}

func TestAssembleThinkingModelInjectsPersonaLockEveryNTurns(t *testing.T) {
	assembler := NewContextAssembler()
	policy := DefaultPromptGuardPolicy()
	policy.PersonaLockEveryTurns = 2
	assembler.UpdatePromptGuardPolicy(policy)

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

func TestPersonaLockIncludesNegativeConstraintOverride(t *testing.T) {
	assembler := NewContextAssembler()
	policy := DefaultPromptGuardPolicy()
	policy.PersonaLockEveryTurns = 1
	policy.NegativeConstraintDefault = "default negative"
	assembler.UpdatePromptGuardPolicy(policy)

	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           domain.PersonaMeta{Name: "Satsuki"},
			SystemTemplate: "{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
			StyleGuard: domain.StyleGuardConfig{
				NegativeConstraints: "custom persona negative",
			},
		},
		Lore: "persona",
	}
	conv := &domain.Conversation{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi", Timestamp: time.Now()}},
	}

	assembled, err := assembler.Assemble(persona, conv, llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    8000,
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	joined := ""
	for _, msg := range assembled.Messages {
		joined += msg.Content + "\n"
	}
	if !strings.Contains(joined, "custom persona negative") {
		t.Fatalf("expected persona-level negative constraint in persona lock message")
	}
}

func TestSelectRecentMessagesSkipsInternalToolHistory(t *testing.T) {
	assembler := NewContextAssembler()
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "[Tool calls executed]"},
		{Role: domain.RoleUser, Content: "Tool execution results:\n\nTool: file_write\nStatus: SUCCESS"},
		{Role: domain.RoleAssistant, Content: "actual assistant response"},
	}

	got := assembler.selectRecentMessages(messages, 10_000, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 non-tool messages, got %d", len(got))
	}
	if got[0].Content != "hello" {
		t.Fatalf("unexpected first message: %q", got[0].Content)
	}
	if got[1].Content != "actual assistant response" {
		t.Fatalf("unexpected second message: %q", got[1].Content)
	}
}
