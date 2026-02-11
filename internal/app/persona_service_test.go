package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonaServiceUpdateUsesOriginalDirName(t *testing.T) {
	baseDir := t.TempDir()
	personaDir := filepath.Join(baseDir, "hayasaka")

	writeTestFile(t, filepath.Join(personaDir, "config.yaml"), `meta:
  id: "hayasaka_silver_beater"
  name: "Hayasaka"
  description: "initial"
  version: "1.0.0"
  author: "test"
system_template: |
  test-template
generation:
  temperature: 0.8
  max_tokens: 256
  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  sanity_level: 10
`)
	writeTestFile(t, filepath.Join(personaDir, "lore.md"), "initial lore\n")
	writeTestFile(t, filepath.Join(personaDir, "user.md"), "initial user profile\n")
	writeTestFile(t, filepath.Join(personaDir, "anchors.yaml"), `anchors:
  - id: "a1"
    tags: ["test"]
    user_input: "hello"
    bot_response: "world"
`)

	service, err := NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	module, ok := service.Get("hayasaka_silver_beater")
	if !ok {
		t.Fatal("expected persona hayasaka_silver_beater to be loaded")
	}
	module.Config.Meta.Description = "updated description"

	if err := service.Update("hayasaka_silver_beater", module); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updatedConfigPath := filepath.Join(personaDir, "config.yaml")
	updatedConfig, err := os.ReadFile(updatedConfigPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !strings.Contains(string(updatedConfig), "updated description") {
		t.Fatalf("expected updated description in %s, got:\n%s", updatedConfigPath, string(updatedConfig))
	}

	if _, err := os.Stat(filepath.Join(baseDir, "hayasaka_silver_beater")); !os.IsNotExist(err) {
		t.Fatalf("unexpected directory created for persona id path: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
