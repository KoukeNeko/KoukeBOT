package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestGetPersonaReturnsRawConfigYAML(t *testing.T) {
	baseDir := t.TempDir()
	writePersonaFixture(t, baseDir, "persona_a", defaultPersonaConfigYAML())

	service, err := app.NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	h := &Handlers{personaService: service}
	req := httptest.NewRequest(http.MethodGet, "/api/personas/persona-a", nil)
	req.SetPathValue("id", "persona-a")
	rec := httptest.NewRecorder()

	h.GetPersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var detail PersonaDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(detail.ConfigYAML, "custom_root:") {
		t.Fatalf("expected config_yaml to include custom_root, got:\n%s", detail.ConfigYAML)
	}
}

func TestUpdatePersonaPersistsRawConfigWithCustomRootKeys(t *testing.T) {
	baseDir := t.TempDir()
	writePersonaFixture(t, baseDir, "persona_a", defaultPersonaConfigYAML())

	service, err := app.NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	h := &Handlers{personaService: service}
	reqBody := PersonaDetail{
		ConfigYAML: `meta:
  id: "persona-a"
  name: "Persona Updated"
  description: "updated"
  version: "2.0.0"
  author: "tester"
system_template: |
  updated template
generation:
  temperature: 0.9

  top_p: 0.9
  frequency_penalty: 0.2
  presence_penalty: 0.2
variables:
  foo: "bar"
custom_root:
  nested:
    keep: true
`,
		Lore:        "new lore\n",
		UserProfile: "new user profile\n",
		Anchors: []domain.Anchor{
			{
				ID:          "a-updated",
				Tags:        []string{"updated"},
				UserInput:   "hello",
				BotResponse: "world",
			},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/personas/persona-a", bytes.NewReader(payload))
	req.SetPathValue("id", "persona-a")
	rec := httptest.NewRecorder()
	h.UpdatePersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	savedConfigPath := filepath.Join(baseDir, "persona_a", "config.yaml")
	savedConfig, err := os.ReadFile(savedConfigPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	content := string(savedConfig)
	if !strings.Contains(content, "custom_root:") || !strings.Contains(content, "keep: true") {
		t.Fatalf("saved config lost custom keys:\n%s", content)
	}
}

func TestUpdatePersonaRejectsInvalidYAML(t *testing.T) {
	baseDir := t.TempDir()
	writePersonaFixture(t, baseDir, "persona_a", defaultPersonaConfigYAML())

	service, err := app.NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	h := &Handlers{personaService: service}
	payload, err := json.Marshal(PersonaDetail{
		ConfigYAML:  "meta: [",
		Lore:        "lore",
		UserProfile: "profile",
		Anchors:     []domain.Anchor{},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/personas/persona-a", bytes.NewReader(payload))
	req.SetPathValue("id", "persona-a")
	rec := httptest.NewRecorder()
	h.UpdatePersona(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUpdatePersonaRejectsMissingRequiredCoreKey(t *testing.T) {
	baseDir := t.TempDir()
	writePersonaFixture(t, baseDir, "persona_a", defaultPersonaConfigYAML())

	service, err := app.NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	h := &Handlers{personaService: service}
	payload, err := json.Marshal(PersonaDetail{
		ConfigYAML: `meta:
  id: "persona-a"
  name: "Persona"
system_template: "x"
generation:
  temperature: 0.8
`,
		Lore:        "lore",
		UserProfile: "profile",
		Anchors:     []domain.Anchor{},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/personas/persona-a", bytes.NewReader(payload))
	req.SetPathValue("id", "persona-a")
	rec := httptest.NewRecorder()
	h.UpdatePersona(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUpdatePersonaAllowsConfigWithoutTimezoneField(t *testing.T) {
	baseDir := t.TempDir()
	writePersonaFixture(t, baseDir, "persona_a", defaultPersonaConfigYAML())

	service, err := app.NewPersonaService(baseDir)
	if err != nil {
		t.Fatalf("NewPersonaService: %v", err)
	}

	h := &Handlers{personaService: service}
	payload, err := json.Marshal(PersonaDetail{
		ConfigYAML: `meta:
  id: "persona-a"
  name: "Persona A"
  description: "desc"
  version: "1.0.0"
  author: "tester"
system_template: |
  hi
generation:
  temperature: 0.8

  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables: {}
custom_root: "ok"
`,
		Lore:        "lore",
		UserProfile: "profile",
		Anchors:     []domain.Anchor{},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/personas/persona-a", bytes.NewReader(payload))
	req.SetPathValue("id", "persona-a")
	rec := httptest.NewRecorder()
	h.UpdatePersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func writePersonaFixture(t *testing.T, baseDir, dirName, configYAML string) {
	t.Helper()
	personaDir := filepath.Join(baseDir, dirName)
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("mkdir persona dir: %v", err)
	}
	writeFile(t, filepath.Join(personaDir, "config.yaml"), configYAML)
	writeFile(t, filepath.Join(personaDir, "lore.md"), "lore\n")
	writeFile(t, filepath.Join(personaDir, "user.md"), "user profile\n")
	writeFile(t, filepath.Join(personaDir, "anchors.yaml"), "anchors: []\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func defaultPersonaConfigYAML() string {
	return `meta:
  id: "persona-a"
  name: "Persona A"
  description: "desc"
  version: "1.0.0"
  author: "tester"
system_template: |
  hello
generation:
  temperature: 0.8

  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0.1
variables:
  foo: "bar"
custom_root:
  nested: true
`
}
