package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const (
	defaultGeminiCLIBinary = "gemini"
	geminiCLIOutputFormat  = "stream-json"
)

const geminiCLIAuthGuidance = "gemini CLI auth is not configured on server (missing ~/.gemini/settings.json and env GEMINI_API_KEY/GOOGLE_GENAI_USE_VERTEXAI/GOOGLE_GENAI_USE_GCA). For OAuth pool accounts, select provider google-antigravity"

// GeminiCLIProvider executes the local Gemini CLI in headless mode.
// It mirrors OpenClaw's google-gemini-cli direction (local CLI runtime).
type GeminiCLIProvider struct {
	binary string
	model  string
	tools  []domain.Tool
}

type geminiCLIStreamEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Delta   bool   `json:"delta"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Stats   struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"stats"`
}

// NewGeminiCLIProvider creates a provider backed by the local `gemini` command.
func NewGeminiCLIProvider(model string) (*GeminiCLIProvider, error) {
	binary := strings.TrimSpace(os.Getenv("GEMINI_CLI_BIN"))
	if binary == "" {
		binary = defaultGeminiCLIBinary
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("gemini CLI binary %q not found in PATH: %w", binary, err)
	}
	if err := validateGeminiCLIAuthConfigured(); err != nil {
		return nil, err
	}

	normalizedModel := NormalizeGoogleModelAlias(model)
	if normalizedModel == "" {
		normalizedModel = googleProviderSpec(ProviderGoogleGeminiCLI).DefaultModel
	}

	return &GeminiCLIProvider{
		binary: binary,
		model:  normalizedModel,
	}, nil
}

// WithModel returns a shallow clone configured for another model.
func (g *GeminiCLIProvider) WithModel(model string) Provider {
	return &GeminiCLIProvider{
		binary: g.binary,
		model:  NormalizeGoogleModelAlias(model),
		tools:  append([]domain.Tool(nil), g.tools...),
	}
}

func (g *GeminiCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	ch, err := g.StreamChat(ctx, req)
	if err != nil {
		return nil, err
	}

	var text strings.Builder
	tokens := 0
	for chunk := range ch {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		if chunk.Content != "" {
			text.WriteString(chunk.Content)
		}
		if chunk.Done {
			tokens = chunk.TokensUsed
		}
	}

	return &ChatResponse{
		Content:      text.String(),
		FinishReason: "STOP",
		TokensUsed:   tokens,
	}, nil
}

func (g *GeminiCLIProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	prompt := buildGeminiCLIPrompt(req.SystemPrompt, req.Messages)
	args := []string{
		"--prompt", "",
		"--model", g.model,
		"--output-format", geminiCLIOutputFormat,
		"--approval-mode", "yolo",
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)

		cmd := exec.CommandContext(ctx, g.binary, args...)
		cmd.Stdin = strings.NewReader(prompt)
		rawOutput, err := cmd.CombinedOutput()

		totalTokens := 0
		var parseErr error
		sawResult := false
		lines := strings.Split(string(rawOutput), "\n")
		for _, line := range lines {
			event, ok := parseGeminiCLIStreamEvent(line)
			if !ok {
				continue
			}

			switch strings.ToLower(strings.TrimSpace(event.Type)) {
			case "message":
				if strings.EqualFold(strings.TrimSpace(event.Role), "assistant") && event.Content != "" {
					ch <- StreamChunk{Content: event.Content}
				}
			case "result":
				if strings.EqualFold(strings.TrimSpace(event.Status), "success") {
					sawResult = true
					totalTokens = event.Stats.TotalTokens
					continue
				}
				parseErr = fmt.Errorf("gemini CLI result status %q: %s", event.Status, strings.TrimSpace(event.Message))
			}
		}

		if parseErr != nil {
			ch <- StreamChunk{Error: parseErr, Done: true}
			return
		}

		if err != nil {
			ch <- StreamChunk{Error: formatGeminiCLICommandError(g.model, err, rawOutput), Done: true}
			return
		}
		if !sawResult {
			ch <- StreamChunk{Error: fmt.Errorf("gemini CLI produced no stream result"), Done: true}
			return
		}

		ch <- StreamChunk{Done: true, TokensUsed: totalTokens, FinishReason: "STOP"}
	}()

	return ch, nil
}

func (g *GeminiCLIProvider) StreamChatMultimodal(
	ctx context.Context,
	systemPrompt string,
	messages []domain.MultimodalMessage,
	generation domain.GenerationConfig,
) (<-chan StreamChunk, error) {
	flatMessages := flattenMultimodalForGeminiCLI(messages)
	return g.StreamChat(ctx, ChatRequest{
		SystemPrompt: systemPrompt,
		Messages:     flatMessages,
		Generation:   generation,
		ToolMode:     ToolModeDisabled,
	})
}

func (g *GeminiCLIProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     1_000_000,
		MaxOutputTokens:   resolveMaxTokensForModel(g.model, defaultMaxOutputTokens),
		SupportsStreaming: true,
		ProviderName:      "gemini-cli/local",
		Model:             g.model,
	}
}

func (g *GeminiCLIProvider) RegisterTools(tools []domain.Tool) {
	// Local Gemini CLI tool execution is managed internally by the CLI runtime.
	// Keep the list for diagnostics/future policy wiring.
	g.tools = append([]domain.Tool(nil), tools...)
}

func parseGeminiCLIStreamEvent(line string) (geminiCLIStreamEvent, bool) {
	var event geminiCLIStreamEvent
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return event, false
	}
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return event, false
	}
	if strings.TrimSpace(event.Type) == "" {
		return event, false
	}
	return event, true
}

func formatGeminiCLICommandError(model string, cmdErr error, rawOutput []byte) error {
	out := strings.TrimSpace(string(rawOutput))
	lower := strings.ToLower(out)

	// Normalize quota/capacity signals so pool classifiers can detect them.
	if strings.Contains(lower, "no capacity available") ||
		strings.Contains(lower, "model_capacity_exhausted") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "you have exhausted your capacity") {
		return fmt.Errorf("gemini cli HTTP 429: No capacity available for model %s on the server", model)
	}
	if strings.Contains(lower, "please set an auth method") ||
		strings.Contains(lower, "gemini_api_key") ||
		strings.Contains(lower, "google_genai_use_vertexai") ||
		strings.Contains(lower, "google_genai_use_gca") {
		return fmt.Errorf(geminiCLIAuthGuidance)
	}

	if out == "" {
		return fmt.Errorf("gemini CLI command failed: %w", cmdErr)
	}

	preview := summarizeGeminiCLIOutput(out, 1200)
	return fmt.Errorf("gemini CLI command failed: %v: %s", cmdErr, preview)
}

func validateGeminiCLIAuthConfigured() error {
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" {
		return nil
	}
	if envBoolEnabled("GOOGLE_GENAI_USE_VERTEXAI") || envBoolEnabled("GOOGLE_GENAI_USE_GCA") {
		return nil
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		if _, statErr := os.Stat(filepath.Join(home, ".gemini", "settings.json")); statErr == nil {
			return nil
		}
	}
	return fmt.Errorf(geminiCLIAuthGuidance)
}

func envBoolEnabled(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func summarizeGeminiCLIOutput(raw string, maxLen int) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	if maxLen <= 0 || len(clean) <= maxLen {
		return clean
	}
	return clean[:maxLen] + "..."
}

func buildGeminiCLIPrompt(systemPrompt string, messages []domain.Message) string {
	var b strings.Builder

	system := strings.TrimSpace(systemPrompt)
	if system != "" {
		b.WriteString("[System]\n")
		b.WriteString(system)
		b.WriteString("\n\n")
	}

	b.WriteString("[Conversation]\n")
	for _, msg := range messages {
		role := normalizeRoleLabel(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	b.WriteString("assistant: ")

	return b.String()
}

func flattenMultimodalForGeminiCLI(messages []domain.MultimodalMessage) []domain.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]domain.Message, 0, len(messages))
	for _, msg := range messages {
		var parts []string
		for _, part := range msg.Parts {
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
			if part.ImageData != nil {
				filename := strings.TrimSpace(part.ImageData.Filename)
				if filename == "" {
					filename = "image"
				}
				mime := strings.TrimSpace(part.ImageData.MimeType)
				if mime == "" {
					mime = "application/octet-stream"
				}
				parts = append(parts, fmt.Sprintf(
					"[Image attachment: %s (%s, %d bytes). Local Gemini CLI mode does not pass binary image bytes directly.]",
					filename,
					mime,
					len(part.ImageData.Data),
				))
			}
		}

		combined := strings.TrimSpace(strings.Join(parts, "\n"))
		out = append(out, domain.Message{
			Role:    msg.Role,
			Content: combined,
		})
	}

	return out
}

func normalizeRoleLabel(role domain.Role) string {
	switch role {
	case domain.RoleAssistant:
		return "assistant"
	case domain.RoleSystem:
		return "system"
	default:
		return "user"
	}
}

// parseGeminiCLIJSONOutput extracts response text/tokens from non-stream JSON mode.
// Kept for tests/debugging if we need to switch output mode later.
func parseGeminiCLIJSONOutput(raw []byte) (content string, tokens int, err error) {
	type tokensPayload struct {
		Total int `json:"total"`
	}
	type modelStats struct {
		Tokens tokensPayload `json:"tokens"`
	}
	type statsPayload struct {
		Models map[string]modelStats `json:"models"`
	}
	type responsePayload struct {
		Response string       `json:"response"`
		Stats    statsPayload `json:"stats"`
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	var payload responsePayload
	if err := decoder.Decode(&payload); err != nil {
		return "", 0, err
	}
	total := 0
	for _, stats := range payload.Stats.Models {
		if stats.Tokens.Total > total {
			total = stats.Tokens.Total
		}
	}
	return payload.Response, total, nil
}

// scanGeminiCLIOutputLines scans raw command output safely for future diagnostics.
func scanGeminiCLIOutputLines(raw []byte, fn func(string)) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		fn(scanner.Text())
	}
}
