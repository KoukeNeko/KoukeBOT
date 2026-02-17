package llm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestParseGeminiCLIStreamEvent(t *testing.T) {
	event, ok := parseGeminiCLIStreamEvent(`{"type":"message","role":"assistant","content":"hello","delta":true}`)
	if !ok {
		t.Fatalf("expected JSON event to be parsed")
	}
	if event.Type != "message" {
		t.Fatalf("event type = %q, want message", event.Type)
	}
	if event.Content != "hello" {
		t.Fatalf("event content = %q, want hello", event.Content)
	}

	if _, ok := parseGeminiCLIStreamEvent(`{"role":"assistant","content":"hello"}`); ok {
		t.Fatalf("expected event without type to be ignored")
	}
	if _, ok := parseGeminiCLIStreamEvent(`not-json`); ok {
		t.Fatalf("expected non-json line to be ignored")
	}
}

func TestFormatGeminiCLICommandErrorCapacityNormalization(t *testing.T) {
	err := formatGeminiCLICommandError("gemini-3-pro-preview", errors.New("exit status 1"), []byte(`RESOURCE_EXHAUSTED MODEL_CAPACITY_EXHAUSTED`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("expected normalized 429 error, got: %v", err)
	}
}

func TestFormatGeminiCLICommandErrorAuthGuidance(t *testing.T) {
	raw := []byte("Please set an Auth method in your /home/node/.gemini/settings.json or specify GEMINI_API_KEY")
	err := formatGeminiCLICommandError("gemini-2.5-pro", errors.New("exit status 41"), raw)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "gemini CLI auth is not configured on server") {
		t.Fatalf("expected auth guidance error, got: %v", err)
	}
}

func TestValidateGeminiCLIAuthConfiguredByEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "")
	t.Setenv("GOOGLE_GENAI_USE_GCA", "")
	if err := validateGeminiCLIAuthConfigured(); err != nil {
		t.Fatalf("expected env auth to pass, got: %v", err)
	}
}

func TestValidateGeminiCLIAuthConfiguredBySettingsFile(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "")
	t.Setenv("GOOGLE_GENAI_USE_GCA", "")

	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(`{"auth":"ok"}`), 0o644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	if err := validateGeminiCLIAuthConfigured(); err != nil {
		t.Fatalf("expected settings file auth to pass, got: %v", err)
	}
}

func TestValidateGeminiCLIAuthConfiguredMissing(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "")
	t.Setenv("GOOGLE_GENAI_USE_GCA", "")
	t.Setenv("HOME", t.TempDir())

	err := validateGeminiCLIAuthConfigured()
	if err == nil {
		t.Fatalf("expected missing auth error")
	}
	if !strings.Contains(err.Error(), "google-antigravity") {
		t.Fatalf("expected guidance to google-antigravity, got: %v", err)
	}
}

func TestFlattenMultimodalForGeminiCLI(t *testing.T) {
	messages := flattenMultimodalForGeminiCLI([]domain.MultimodalMessage{
		{
			Role: domain.RoleUser,
			Parts: []domain.MessagePart{
				{Text: "analyze this"},
				{
					ImageData: &domain.ImageInput{
						Filename: "a.png",
						MimeType: "image/png",
						Data:     []byte{1, 2, 3},
					},
				},
			},
		},
	})

	if len(messages) != 1 {
		t.Fatalf("flattened len = %d, want 1", len(messages))
	}
	if !strings.Contains(messages[0].Content, "analyze this") {
		t.Fatalf("expected text part in flattened content, got: %q", messages[0].Content)
	}
	if !strings.Contains(messages[0].Content, "Image attachment: a.png") {
		t.Fatalf("expected image placeholder in flattened content, got: %q", messages[0].Content)
	}
}

func TestBuildGeminiCLIPrompt(t *testing.T) {
	prompt := buildGeminiCLIPrompt("sys", []domain.Message{
		{Role: domain.RoleUser, Content: "u1"},
		{Role: domain.RoleAssistant, Content: "a1"},
	})
	if !strings.Contains(prompt, "[System]") {
		t.Fatalf("expected system section in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "user: u1") {
		t.Fatalf("expected user message in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "assistant: a1") {
		t.Fatalf("expected assistant message in prompt: %q", prompt)
	}
}
