package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/persona"
)

func TestSavePreservesComments(t *testing.T) {
	// Copy the real persona to a temp dir
	src := filepath.Join("..", "configs", "personas", "hayasaka")
	tmpDir := t.TempDir()
	personaDir := filepath.Join(tmpDir, "hayasaka")

	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"config.yaml", "lore.md", "anchors.yaml"} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(personaDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Load, modify, save
	module, err := persona.LoadPersona(tmpDir, "hayasaka")
	if err != nil {
		t.Fatal(err)
	}

	module.Config.Meta.Name = "Modified Name"
	module.Config.Generation.Temperature = 0.99

	if err := persona.SavePersona(tmpDir, "hayasaka", module); err != nil {
		t.Fatal(err)
	}

	// Read back and verify comments are preserved
	saved, err := os.ReadFile(filepath.Join(personaDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(saved)

	// Verify value was updated
	if !strings.Contains(content, "Modified Name") {
		t.Error("expected updated name in saved file")
	}

	// Verify comments are preserved
	comments := []string{
		"# 系統提示詞模板",
		"# 稍微調高以增加",
		"# 生成參數設定",
		"# 狀態變數",
	}
	for _, c := range comments {
		if !strings.Contains(content, c) {
			t.Errorf("comment lost: %q", c)
		}
	}

	// Verify roundtrip produces valid config
	reloaded, err := persona.LoadPersona(tmpDir, "hayasaka")
	if err != nil {
		t.Fatalf("reload after save failed: %v", err)
	}
	if reloaded.Config.Meta.Name != "Modified Name" {
		t.Errorf("name = %q, want %q", reloaded.Config.Meta.Name, "Modified Name")
	}
	if reloaded.Config.Generation.Temperature != 0.99 {
		t.Errorf("temperature = %v, want 0.99", reloaded.Config.Generation.Temperature)
	}

	// Also verify anchors roundtrip
	if len(reloaded.Anchors) != len(module.Anchors) {
		t.Errorf("anchors count = %d, want %d", len(reloaded.Anchors), len(module.Anchors))
	}

	// Also verify lore roundtrip
	if reloaded.Lore != module.Lore {
		t.Error("lore content changed after roundtrip")
	}
}

func TestSaveNewFileWithoutExisting(t *testing.T) {
	tmpDir := t.TempDir()
	personaDir := filepath.Join(tmpDir, "new_persona")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	module := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta: domain.PersonaMeta{
				ID:   "test",
				Name: "Test Persona",
			},
			Generation: domain.GenerationConfig{
				Temperature: 0.7,
			},
		},
		Lore:    "# Test lore",
		Anchors: []domain.Anchor{},
	}

	if err := persona.SavePersona(tmpDir, "new_persona", module); err != nil {
		t.Fatal(err)
	}

	reloaded, err := persona.LoadPersona(tmpDir, "new_persona")
	if err != nil {
		t.Fatalf("load new persona: %v", err)
	}
	if reloaded.Config.Meta.Name != "Test Persona" {
		t.Errorf("name = %q, want %q", reloaded.Config.Meta.Name, "Test Persona")
	}
}
