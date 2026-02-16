package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/persona"
)

func findPersonasDir(t *testing.T) string {
	t.Helper()
	// Walk up from test location to find configs/personas
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "configs", "personas")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find configs/personas directory")
	return ""
}

func TestPersonaLoaderLoadsHayasaka(t *testing.T) {
	personasDir := findPersonasDir(t)

	module, err := persona.LoadPersona(personasDir, "hayasaka")
	if err != nil {
		t.Fatalf("LoadPersona failed: %v", err)
	}

	// Verify config.yaml parsed correctly
	if module.Config.Meta.ID == "" {
		t.Error("meta.id is empty")
	}
	if module.Config.Meta.Name == "" {
		t.Error("meta.name is empty")
	}
	if module.Config.SystemTemplate == "" {
		t.Error("system_template is empty")
	}
	if !strings.Contains(module.Config.SystemTemplate, "{{.Lore}}") {
		t.Error("system_template missing {{.Lore}} injection point")
	}
	if !strings.Contains(module.Config.SystemTemplate, "{{.Anchors}}") {
		t.Error("system_template missing {{.Anchors}} injection point")
	}
	if !strings.Contains(module.Config.SystemTemplate, "{{.Time}}") {
		t.Error("system_template missing {{.Time}} injection point")
	}

	// Verify generation config
	if module.Config.Generation.Temperature <= 0 {
		t.Error("generation.temperature should be > 0")
	}

	// Verify lore.md loaded
	if module.Lore == "" {
		t.Error("lore is empty")
	}
	if !strings.Contains(module.Lore, "早坂愛") {
		t.Error("lore should mention 早坂愛")
	}

	// Verify anchors.yaml loaded
	if len(module.Anchors) == 0 {
		t.Error("anchors is empty")
	}
	for i, anchor := range module.Anchors {
		if anchor.ID == "" {
			t.Errorf("anchor[%d] has empty id", i)
		}
		if anchor.UserInput == "" {
			t.Errorf("anchor[%d] %q has empty user_input", i, anchor.ID)
		}
		if anchor.BotResponse == "" {
			t.Errorf("anchor[%d] %q has empty bot_response", i, anchor.ID)
		}
	}

	t.Logf("persona %q loaded: %d anchors, lore %d bytes",
		module.Config.Meta.ID, len(module.Anchors), len(module.Lore))
}

func TestLoadAllPersonas(t *testing.T) {
	personasDir := findPersonasDir(t)

	personas, err := persona.LoadAllPersonas(personasDir)
	if err != nil {
		t.Fatalf("LoadAllPersonas failed: %v", err)
	}
	if len(personas) == 0 {
		t.Fatal("no personas loaded")
	}

	for id, p := range personas {
		t.Logf("loaded: %s (%s v%s)", id, p.Config.Meta.Name, p.Config.Meta.Version)
	}
}

// mockCapabilities returns ModelCapabilities for testing.
func thinkingCaps() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking: true,
		ContextWindow:    1_000_000,
		SupportsStreaming: true,
		ProviderName:     "test-thinking",
	}
}

func legacyCaps() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking: false,
		ContextWindow:    32_000,
		SupportsStreaming: true,
		ProviderName:     "test-legacy",
	}
}

func TestAssemblerThinkingModel(t *testing.T) {
	personasDir := findPersonasDir(t)
	module, err := persona.LoadPersona(personasDir, "hayasaka")
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}

	conv := &domain.Conversation{
		ID:        "test",
		ChannelID: "ch-1",
		PersonaID: module.Config.Meta.ID,
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "早安", Timestamp: time.Now()},
		},
	}

	assembler := app.NewContextAssembler()
	result, err := assembler.Assemble(module, conv, thinkingCaps())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// System prompt should contain lore (injected via {{.Lore}})
	if !strings.Contains(result.SystemPrompt, "早坂愛") {
		t.Error("system prompt missing lore content (早坂愛)")
	}

	// System prompt should contain anchor text (injected via {{.Anchors}})
	if !strings.Contains(result.SystemPrompt, "Example 1") {
		t.Error("system prompt missing anchors injection")
	}

	// For thinking models, messages are raw history only (no duplicate anchors)
	if len(result.Messages) != 1 {
		t.Errorf("thinking model should have 1 message (raw history), got %d", len(result.Messages))
	}

	t.Logf("thinking model system prompt: %d chars, %d messages",
		len(result.SystemPrompt), len(result.Messages))
}

func TestAssemblerLegacyModel(t *testing.T) {
	personasDir := findPersonasDir(t)
	module, err := persona.LoadPersona(personasDir, "hayasaka")
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}

	conv := &domain.Conversation{
		ID:        "test",
		ChannelID: "ch-1",
		PersonaID: module.Config.Meta.ID,
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "早安", Timestamp: time.Now()},
		},
	}

	assembler := app.NewContextAssembler()
	result, err := assembler.Assemble(module, conv, legacyCaps())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	anchorCount := len(module.Anchors)
	// Legacy model: anchors as user/assistant pairs + conversation history
	expectedMinMessages := anchorCount*2 + 1
	if len(result.Messages) < expectedMinMessages {
		t.Errorf("legacy model should have at least %d messages (anchors*2 + history), got %d",
			expectedMinMessages, len(result.Messages))
	}

	// First message pair should be from anchors
	if len(result.Messages) >= 2 {
		if result.Messages[0].Role != domain.RoleUser {
			t.Error("first message should be user (anchor)")
		}
		if result.Messages[1].Role != domain.RoleAssistant {
			t.Error("second message should be assistant (anchor)")
		}
	}

	t.Logf("legacy model system prompt: %d chars, %d messages (anchors: %d pairs)",
		len(result.SystemPrompt), len(result.Messages), anchorCount)
}
