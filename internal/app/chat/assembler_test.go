package chat

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
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

func TestRenderSystemPromptIncludesSystemInstructions(t *testing.T) {
	assembler := NewContextAssembler()
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "TOON schema test {{.Lore}} {{.Anchors}} {{.Time}}",
		},
		Lore: "persona lore",
	}

	got, err := assembler.renderSystemPrompt(persona, DefaultPromptGuardPolicy())
	if err != nil {
		t.Fatalf("renderSystemPrompt() error = %v", err)
	}

	wants := []string{
		"Tool results are encoded in TOON (`t=toolr`)",
		"`items[]` where each item has `id`, `name`, `ok`, `out`",
		"Channel history appears as user/assistant turns",
		"[Name · HH:mm]",
		"[↩ Name · HH:mm]",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt should include %q, got:\n%s", want, got)
		}
	}
}

func TestRenderSystemPromptUsesGlobalTimezone(t *testing.T) {
	assembler := NewContextAssembler()
	assembler.UpdateGlobalTimezone("Asia/Taipei")
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "Current Time: {{.Time}}",
		},
	}

	got, err := assembler.renderSystemPrompt(persona, DefaultPromptGuardPolicy())
	if err != nil {
		t.Fatalf("renderSystemPrompt() error = %v", err)
	}
	if !strings.Contains(got, "Time zone: Asia/Taipei") {
		t.Fatalf("expected global timezone in system prompt, got:\n%s", got)
	}
}

func TestRenderSystemPromptUsesGlobalTimezoneFallback(t *testing.T) {
	assembler := NewContextAssembler()
	assembler.UpdateGlobalTimezone("America/New_York")
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "Current Time: {{.Time}}",
		},
	}

	got, err := assembler.renderSystemPrompt(persona, DefaultPromptGuardPolicy())
	if err != nil {
		t.Fatalf("renderSystemPrompt() error = %v", err)
	}
	if !strings.Contains(got, "Time zone: America/New_York") {
		t.Fatalf("expected global timezone fallback in system prompt, got:\n%s", got)
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

func TestSelectRecentMessagesSkipsTOONToolHistory(t *testing.T) {
	assembler := NewContextAssembler()
	toolr, err := tooncodec.EncodeToolResultsEvent([]tooncodec.ToolResultInput{
		{ID: "1", Name: "file_write", OK: true, Out: "saved"},
	})
	if err != nil {
		t.Fatalf("encode toolr: %v", err)
	}

	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleUser, Content: toolr},
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

func TestAssembleLegacyModelUsesConfiguredRecentWindow(t *testing.T) {
	assembler := NewContextAssembler()
	// 20 user turns → keeps last 20 user+assistant pairs = 40 messages
	assembler.UpdateMaxUserTurns(20)

	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "legacy test {{.Lore}} {{.Anchors}} {{.Time}}",
		},
		Lore: "legacy",
	}
	conv := &domain.Conversation{Messages: make([]domain.Message, 0, 60)}
	for i := 0; i < 60; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("msg-%02d", i),
			Timestamp: time.Now(),
		})
	}

	assembled, err := assembler.Assemble(persona, conv, llm.ModelCapabilities{SupportsThinking: false})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if len(assembled.Messages) != 40 {
		t.Fatalf("assembled legacy messages = %d, want 40", len(assembled.Messages))
	}
	if assembled.Messages[0].Content != "msg-20" {
		t.Fatalf("legacy first recent message = %q, want msg-20", assembled.Messages[0].Content)
	}
	if assembled.Messages[39].Content != "msg-59" {
		t.Fatalf("legacy last recent message = %q, want msg-59", assembled.Messages[39].Content)
	}
}

func TestAssembleThinkingModelUsesConfiguredRecentWindow(t *testing.T) {
	assembler := NewContextAssembler()
	// 20 user turns → keeps last 20 user+assistant pairs = 40 messages
	assembler.UpdateMaxUserTurns(20)
	policy := DefaultPromptGuardPolicy()
	policy.PersonaLockEveryTurns = 999
	assembler.UpdatePromptGuardPolicy(policy)

	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			SystemTemplate: "thinking test {{.Lore}} {{.Anchors}} {{.Time}}",
		},
		Lore: "thinking",
	}
	conv := &domain.Conversation{Messages: make([]domain.Message, 0, 80)}
	for i := 0; i < 80; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("turn-%02d", i),
			Timestamp: time.Now(),
		})
	}

	assembled, err := assembler.Assemble(persona, conv, llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    64_000,
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}

	// No digest/persona-lock expected here, so count should match configured recent window.
	if len(assembled.Messages) != 40 {
		t.Fatalf("assembled thinking messages = %d, want 40", len(assembled.Messages))
	}
	if assembled.Messages[0].Content != "turn-40" {
		t.Fatalf("thinking first recent message = %q, want turn-40", assembled.Messages[0].Content)
	}
	if assembled.Messages[39].Content != "turn-79" {
		t.Fatalf("thinking last recent message = %q, want turn-79", assembled.Messages[39].Content)
	}
}

func TestSelectRecentMessages_CountsUserTurnsNotRawMessages(t *testing.T) {
	assembler := NewContextAssembler()
	// Simulate tool-heavy conversation: each user turn has 3 tool result messages.
	toolr, _ := tooncodec.EncodeToolResultsEvent([]tooncodec.ToolResultInput{
		{ID: "1", Name: "web_search", OK: true, Out: "result"},
	})
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "turn 1"},
		{Role: domain.RoleUser, Content: toolr},
		{Role: domain.RoleAssistant, Content: "reply 1"},
		{Role: domain.RoleUser, Content: "turn 2"},
		{Role: domain.RoleUser, Content: toolr},
		{Role: domain.RoleAssistant, Content: "reply 2"},
		{Role: domain.RoleUser, Content: "turn 3"},
		{Role: domain.RoleUser, Content: toolr},
		{Role: domain.RoleAssistant, Content: "reply 3"},
	}

	// maxUserTurns=2: should keep turns 2 and 3 (plus their tool results and replies)
	got := assembler.selectRecentMessages(messages, 100_000, 2)

	// After filtering internal tool messages, expect: turn 2, reply 2, turn 3, reply 3
	if len(got) != 4 {
		var contents []string
		for _, m := range got {
			contents = append(contents, fmt.Sprintf("%s:%s", m.Role, m.Content[:min(30, len(m.Content))]))
		}
		t.Fatalf("expected 4 messages (2 turns), got %d: %v", len(got), contents)
	}
	if got[0].Content != "turn 2" {
		t.Fatalf("first message should be 'turn 2', got %q", got[0].Content)
	}
	if got[3].Content != "reply 3" {
		t.Fatalf("last message should be 'reply 3', got %q", got[3].Content)
	}
}

func TestSelectRecentMessages_PreservesAssociatedToolMessages(t *testing.T) {
	assembler := NewContextAssembler()
	// Non-TOON tool results (not filtered by isInternalToolHistoryMessage) should be preserved.
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "user 1"},
		{Role: domain.RoleAssistant, Content: "I'll search for that"},
		{Role: domain.RoleAssistant, Content: "here are the search results"},
		{Role: domain.RoleUser, Content: "user 2"},
		{Role: domain.RoleAssistant, Content: "final reply"},
	}

	got := assembler.selectRecentMessages(messages, 100_000, 1)
	// maxUserTurns=1: keep only last user turn (user 2 + final reply)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages for 1 user turn, got %d", len(got))
	}
	if got[0].Content != "user 2" {
		t.Fatalf("first should be 'user 2', got %q", got[0].Content)
	}
}
