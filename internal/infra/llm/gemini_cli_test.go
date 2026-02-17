package llm

import (
	"errors"
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
