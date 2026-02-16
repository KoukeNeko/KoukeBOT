package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestLoadPersonaConfigRaw(t *testing.T) {
	baseDir := t.TempDir()
	personaDir := filepath.Join(baseDir, "persona_a")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("mkdir persona dir: %v", err)
	}

	raw := "meta:\n  id: \"persona-a\"\n  name: \"Persona\"\nsystem_template: \"x\"\ngeneration: {}\nvariables: {}\ncustom: true\n"
	if err := os.WriteFile(filepath.Join(personaDir, "config.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := LoadPersonaConfigRaw(baseDir, "persona_a")
	if err != nil {
		t.Fatalf("LoadPersonaConfigRaw: %v", err)
	}
	if got != raw {
		t.Fatalf("raw config mismatch\nwant:\n%s\ngot:\n%s", raw, got)
	}
}

func TestSavePersonaWithRawConfigWritesExactConfigAndKeepsCustomKeys(t *testing.T) {
	baseDir := t.TempDir()
	raw := `meta:
  id: "persona-updated"
  name: "Updated Persona"
  description: "desc"
  version: "1.1.0"
  author: "tester"
system_template: |
  hello
generation:
  temperature: 0.8
  max_tokens: 512
  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  foo: "bar"
custom_root:
  nested: 1
`

	module := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta: domain.PersonaMeta{
				ID:   "persona-updated",
				Name: "Updated Persona",
			},
		},
		Lore:        "updated lore\n",
		UserProfile: "updated user\n",
		Anchors: []domain.Anchor{
			{
				ID:          "a1",
				Tags:        []string{"test"},
				UserInput:   "hi",
				BotResponse: "hello",
			},
		},
	}

	if err := SavePersonaWithRawConfig(baseDir, "persona_a", raw, module); err != nil {
		t.Fatalf("SavePersonaWithRawConfig: %v", err)
	}

	configPath := filepath.Join(baseDir, "persona_a", "config.yaml")
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if string(saved) != raw {
		t.Fatalf("saved raw config mismatch\nwant:\n%s\ngot:\n%s", raw, string(saved))
	}
	if !strings.Contains(string(saved), "custom_root:") {
		t.Fatalf("expected custom_root in saved config")
	}
}
