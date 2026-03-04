package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const (
	defaultAnthropicEndpoint      = "https://api.anthropic.com"
	defaultAnthropicVersion       = "2023-06-01"
	defaultAnthropicMaxTokens     = 8192
	defaultAnthropicContextWindow = 200_000

	AnthropicSetupTokenPrefix    = "sk-ant-oat01-"
	AnthropicSetupTokenMinLength = 80
)

var anthropicDefaultBetas = []string{
	"fine-grained-tool-streaming-2025-05-14",
	"interleaved-thinking-2025-05-14",
}

var anthropicOAuthRequiredBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"fine-grained-tool-streaming-2025-05-14",
	"interleaved-thinking-2025-05-14",
}

type AnthropicProvider struct {
	httpClient *http.Client
	endpoint   string
	model      string
	apiKey     string
	bearer     string
	tools      []domain.Tool
}

type anthropicMessagesRequest struct {
	Model       string                  `json:"model"`
	MaxTokens   int                     `json:"max_tokens"`
	System      string                  `json:"system,omitempty"`
	Messages    []anthropicMessage      `json:"messages"`
	Tools       []anthropicToolDef      `json:"tools,omitempty"`
	Temperature *float64                `json:"temperature,omitempty"`
	TopP        *float64                `json:"top_p,omitempty"`
	Stream      bool                    `json:"stream,omitempty"`
	Metadata    map[string]string       `json:"metadata,omitempty"`
	StopSeqs    []string                `json:"stop_sequences,omitempty"`
	ToolChoice  *anthropicToolChoiceAny `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type   string                 `json:"type"`
	Text   string                 `json:"text,omitempty"`
	Source *anthropicImageSource  `json:"source,omitempty"`
	ID     string                 `json:"id,omitempty"`
	Name   string                 `json:"name,omitempty"`
	Input  map[string]interface{} `json:"input,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicToolChoiceAny struct {
	Type string `json:"type"`
}

type anthropicMessagesResponse struct {
	Content []anthropicContentPart `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func ValidateAnthropicSetupToken(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("setup token is required")
	}
	if !strings.HasPrefix(trimmed, AnthropicSetupTokenPrefix) {
		return fmt.Errorf("setup token must start with %s", AnthropicSetupTokenPrefix)
	}
	if len(trimmed) < AnthropicSetupTokenMinLength {
		return fmt.Errorf("setup token looks too short")
	}
	return nil
}

func IsAnthropicSetupToken(raw string) bool {
	return ValidateAnthropicSetupToken(raw) == nil
}

func NewAnthropicAPIProvider(apiKey, model string) (*AnthropicProvider, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if key == "" {
		return nil, fmt.Errorf("no Anthropic API key configured")
	}
	return newAnthropicProvider("", key, model, ProviderAnthropicAPIKey), nil
}

func NewAnthropicSetupTokenProvider(setupToken, model string) (*AnthropicProvider, error) {
	token := strings.TrimSpace(setupToken)
	if err := ValidateAnthropicSetupToken(token); err != nil {
		return nil, err
	}
	return newAnthropicProvider(token, "", model, ProviderAnthropicSetupToken), nil
}

func newAnthropicProvider(setupToken, apiKey, model, providerID string) *AnthropicProvider {
	mid := NormalizeModelAliasForProvider(providerID, model)
	if mid == "" {
		_, mid = SplitProviderModelID(DefaultProviderModelID(providerID))
	}
	endpoint := strings.TrimSpace(strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/"))
	if endpoint == "" {
		endpoint = defaultAnthropicEndpoint
	}
	return &AnthropicProvider{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		endpoint:   endpoint,
		model:      mid,
		apiKey:     strings.TrimSpace(apiKey),
		bearer:     strings.TrimSpace(setupToken),
	}
}

func (a *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resp, err := a.requestMessages(ctx, req.SystemPrompt, req.Messages, req.Generation, req.ToolMode, false)
	if err != nil {
		return nil, err
	}
	text, toolCalls := anthropicResponseToResult(resp)
	if len(toolCalls) > 0 {
		return &ChatResponse{
			Content:    "",
			TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}, nil
	}
	return &ChatResponse{
		Content:      text,
		FinishReason: resp.StopReason,
		TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}, nil
}

func (a *AnthropicProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		resp, err := a.requestMessages(ctx, req.SystemPrompt, req.Messages, req.Generation, req.ToolMode, false)
		if err != nil {
			ch <- StreamChunk{Error: err, Done: true}
			return
		}
		text, toolCalls := anthropicResponseToResult(resp)
		tokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
		if len(toolCalls) > 0 {
			ch <- StreamChunk{
				ToolCalls:    toolCalls,
				Done:         true,
				TokensUsed:   tokens,
				FinishReason: resp.StopReason,
			}
			return
		}
		if text != "" {
			ch <- StreamChunk{Content: text}
		}
		ch <- StreamChunk{Done: true, TokensUsed: tokens, FinishReason: resp.StopReason}
	}()
	return ch, nil
}

func (a *AnthropicProvider) StreamChatMultimodal(
	ctx context.Context,
	systemPrompt string,
	messages []domain.MultimodalMessage,
	generation domain.GenerationConfig,
) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		resp, err := a.requestMessagesMultimodal(ctx, systemPrompt, messages, generation)
		if err != nil {
			ch <- StreamChunk{Error: err, Done: true}
			return
		}
		text, toolCalls := anthropicResponseToResult(resp)
		tokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
		if len(toolCalls) > 0 {
			ch <- StreamChunk{
				ToolCalls:    toolCalls,
				Done:         true,
				TokensUsed:   tokens,
				FinishReason: resp.StopReason,
			}
			return
		}
		if text != "" {
			ch <- StreamChunk{Content: text}
		}
		ch <- StreamChunk{Done: true, TokensUsed: tokens, FinishReason: resp.StopReason}
	}()
	return ch, nil
}

func (a *AnthropicProvider) ModelInfo() ModelCapabilities {
	lower := strings.ToLower(a.model)
	thinking := strings.Contains(lower, "sonnet-4") ||
		strings.Contains(lower, "opus-4")
	providerName := "anthropic-api"
	if strings.TrimSpace(a.bearer) != "" {
		providerName = "anthropic-setup-token"
	}
	return ModelCapabilities{
		SupportsThinking:  thinking,
		ContextWindow:     defaultAnthropicContextWindow,
		MaxOutputTokens:   defaultAnthropicMaxTokens,
		SupportsStreaming: true,
		ProviderName:      providerName,
		Model:             a.model,
	}
}

func (a *AnthropicProvider) RegisterTools(tools []domain.Tool) {
	a.tools = tools
}

func (a *AnthropicProvider) WithModel(model string) Provider {
	cloned := *a
	cloned.model = NormalizeModelAliasForProvider(resolveAnthropicProviderID(a), model)
	return &cloned
}

func resolveAnthropicProviderID(a *AnthropicProvider) string {
	if a == nil {
		return ProviderAnthropicAPIKey
	}
	if strings.TrimSpace(a.bearer) != "" {
		return ProviderAnthropicSetupToken
	}
	return ProviderAnthropicAPIKey
}

func (a *AnthropicProvider) requestMessages(
	ctx context.Context,
	systemPrompt string,
	messages []domain.Message,
	generation domain.GenerationConfig,
	toolMode ToolMode,
	stream bool,
) (*anthropicMessagesResponse, error) {
	parsedSystem, parsedMessages := buildAnthropicMessages(systemPrompt, messages)
	reqBody := anthropicMessagesRequest{
		Model:     a.model,
		MaxTokens: defaultAnthropicMaxTokens,
		System:    parsedSystem,
		Messages:  parsedMessages,
		Stream:    stream,
	}
	if generation.Temperature > 0 {
		reqBody.Temperature = &generation.Temperature
	}
	if generation.TopP > 0 {
		reqBody.TopP = &generation.TopP
	}
	if toolMode != ToolModeDisabled && len(a.tools) > 0 {
		reqBody.Tools = buildAnthropicTools(a.tools)
		reqBody.ToolChoice = &anthropicToolChoiceAny{Type: "auto"}
	}
	return a.postAnthropic(ctx, reqBody)
}

func (a *AnthropicProvider) requestMessagesMultimodal(
	ctx context.Context,
	systemPrompt string,
	messages []domain.MultimodalMessage,
	generation domain.GenerationConfig,
) (*anthropicMessagesResponse, error) {
	reqBody := anthropicMessagesRequest{
		Model:     a.model,
		MaxTokens: defaultAnthropicMaxTokens,
		System:    strings.TrimSpace(systemPrompt),
		Messages:  buildAnthropicMultimodalMessages(messages),
	}
	if generation.Temperature > 0 {
		reqBody.Temperature = &generation.Temperature
	}
	if generation.TopP > 0 {
		reqBody.TopP = &generation.TopP
	}
	return a.postAnthropic(ctx, reqBody)
}

func (a *AnthropicProvider) postAnthropic(ctx context.Context, payload anthropicMessagesRequest) (*anthropicMessagesResponse, error) {
	if len(payload.Messages) == 0 {
		return nil, fmt.Errorf("anthropic request has no messages")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("anthropic marshal request: %w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(a.endpoint), "/")
	if endpoint == "" {
		endpoint = defaultAnthropicEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("anthropic create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "koukebot/1.0")
	req.Header.Set("anthropic-version", defaultAnthropicVersion)
	if strings.TrimSpace(a.bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(a.bearer))
		req.Header.Set("anthropic-beta", strings.Join(anthropicOAuthRequiredBetas, ","))
	} else {
		req.Header.Set("x-api-key", strings.TrimSpace(a.apiKey))
		req.Header.Set("anthropic-beta", strings.Join(anthropicDefaultBetas, ","))
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := parseAnthropicError(body)
		if errMsg == "" {
			errMsg = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, errMsg)
	}
	var parsed anthropicMessagesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic decode response: %w", err)
	}
	return &parsed, nil
}

func parseAnthropicError(body []byte) string {
	var payload anthropicErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return msg
		}
		if typ := strings.TrimSpace(payload.Error.Type); typ != "" {
			return typ
		}
	}
	return strings.TrimSpace(string(body))
}

func buildAnthropicMessages(systemPrompt string, messages []domain.Message) (string, []anthropicMessage) {
	systemParts := make([]string, 0, 4)
	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		systemParts = append(systemParts, trimmed)
	}
	out := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		switch msg.Role {
		case domain.RoleSystem:
			systemParts = append(systemParts, text)
		case domain.RoleAssistant:
			out = append(out, anthropicMessage{
				Role: "assistant",
				Content: []anthropicContentPart{
					{Type: "text", Text: text},
				},
			})
		default:
			out = append(out, anthropicMessage{
				Role: "user",
				Content: []anthropicContentPart{
					{Type: "text", Text: text},
				},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), out
}

func buildAnthropicMultimodalMessages(messages []domain.MultimodalMessage) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "assistant"
		}
		parts := make([]anthropicContentPart, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, anthropicContentPart{
					Type: "text",
					Text: text,
				})
			}
			if part.ImageData != nil && len(part.ImageData.Data) > 0 {
				mime := strings.TrimSpace(part.ImageData.MimeType)
				if mime == "" {
					mime = "image/jpeg"
				}
				parts = append(parts, anthropicContentPart{
					Type: "image",
					Source: &anthropicImageSource{
						Type:      "base64",
						MediaType: mime,
						Data:      base64.StdEncoding.EncodeToString(part.ImageData.Data),
					},
				})
			}
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, anthropicMessage{
			Role:    role,
			Content: parts,
		})
	}
	return out
}

func buildAnthropicTools(tools []domain.Tool) []anthropicToolDef {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicToolDef, 0, len(tools))
	for _, tool := range tools {
		schema := map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
		if raw := tool.InputSchema(); raw != nil {
			schema = raw
		}
		out = append(out, anthropicToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: schema,
		})
	}
	return out
}

func anthropicResponseToResult(resp *anthropicMessagesResponse) (string, []domain.ToolCall) {
	if resp == nil {
		return "", nil
	}
	var textParts []string
	toolCalls := make([]domain.ToolCall, 0, len(resp.Content))
	for idx, block := range resp.Content {
		switch strings.TrimSpace(block.Type) {
		case "text":
			if text := strings.TrimSpace(block.Text); text != "" {
				textParts = append(textParts, text)
			}
		case "tool_use":
			name := strings.TrimSpace(block.Name)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(block.ID)
			if callID == "" {
				callID = fmt.Sprintf("%s-%d", name, idx)
			}
			input := block.Input
			if input == nil {
				input = map[string]interface{}{}
			}
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:    callID,
				Name:  name,
				Input: input,
			})
		}
	}
	return strings.Join(textParts, "\n"), toolCalls
}
