package persona

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

func TestPersonaServiceGetConfigRaw(t *testing.T) {
	baseDir := t.TempDir()
	personaDir := filepath.Join(baseDir, "persona_a")
	rawConfig := `meta:
  id: "persona-a"
  name: "Persona A"
system_template: |
  hello
generation:
  temperature: 0.8

  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  key: "value"
custom_root:
  nested: true
`
	writeTestFile(t, filepath.Join(personaDir, "config.yaml"), rawConfig)
	writeTestFile(t, filepath.Join(personaDir, "lore.md"), "lore\n")
	writeTestFile(t, filepath.Join(personaDir, "user.md"), "user\n")
	writeTestFile(t, filepath.Join(personaDir, "anchors.yaml"), "anchors: []\n")

	service, err := NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	got, err := service.GetConfigRaw("persona-a")
	if err != nil {
		t.Fatalf("GetConfigRaw: %v", err)
	}
	if !strings.Contains(got, "custom_root:") {
		t.Fatalf("expected raw config to include custom_root, got:\n%s", got)
	}
}

func TestPersonaServiceUpdateWithRawConfigPreservesCustomFieldAndRenamesMapKey(t *testing.T) {
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

  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  sanity_level: 10
custom_root:
  keep_me: true
`)
	writeTestFile(t, filepath.Join(personaDir, "lore.md"), "initial lore\n")
	writeTestFile(t, filepath.Join(personaDir, "user.md"), "initial user profile\n")
	writeTestFile(t, filepath.Join(personaDir, "anchors.yaml"), "anchors: []\n")

	service, err := NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	module, ok := service.Get("hayasaka_silver_beater")
	if !ok {
		t.Fatal("expected persona hayasaka_silver_beater to be loaded")
	}
	module.Config.Meta.ID = "hayasaka_renamed"
	module.Config.Meta.Name = "Renamed Hayasaka"

	rawUpdated := `meta:
  id: "hayasaka_renamed"
  name: "Renamed Hayasaka"
  description: "updated"
  version: "2.0.0"
  author: "test"
system_template: |
  updated-template
generation:
  temperature: 0.9

  top_p: 0.9
  frequency_penalty: 0.2
  presence_penalty: 0.2
variables:
  sanity_level: 11
custom_root:
  keep_me: true
  extra: "custom"
`

	if err := service.UpdateWithRawConfig("hayasaka_silver_beater", rawUpdated, module); err != nil {
		t.Fatalf("UpdateWithRawConfig: %v", err)
	}

	updatedConfigPath := filepath.Join(personaDir, "config.yaml")
	updatedConfig, err := os.ReadFile(updatedConfigPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	content := string(updatedConfig)
	if !strings.Contains(content, "custom_root:") || !strings.Contains(content, "extra: \"custom\"") {
		t.Fatalf("custom fields were not preserved in raw config:\n%s", content)
	}

	if _, ok := service.Get("hayasaka_silver_beater"); ok {
		t.Fatalf("expected old persona id key to be removed")
	}
	if _, ok := service.Get("hayasaka_renamed"); !ok {
		t.Fatalf("expected new persona id key to be present")
	}

	if _, err := os.Stat(filepath.Join(baseDir, "hayasaka_renamed")); !os.IsNotExist(err) {
		t.Fatalf("unexpected directory created for renamed persona id: %v", err)
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
