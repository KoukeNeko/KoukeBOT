package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"golang.org/x/oauth2"
)

// Cloud Code Assist API — the endpoint used by Gemini CLI OAuth.
// Reference: github.com/openclaw/openclaw
const codeAssistEndpoint = "https://cloudcode-pa.googleapis.com"

// GeminiOAuthProvider uses OAuth token + Cloud Code Assist API to access Gemini.
// The genai SDK requires an API key for BackendGeminiAPI, so we bypass it
// and make raw HTTP calls to cloudcode-pa.googleapis.com with Bearer auth.
type GeminiOAuthProvider struct {
	httpClient *http.Client
	model      string
	projectID  string
}

// NewGeminiOAuthProvider creates a provider using OAuth credentials via Cloud Code Assist API.
func NewGeminiOAuthProvider(ctx context.Context, tokenData *OAuthTokenData, model string) (*GeminiOAuthProvider, error) {
	tokenSource := NewTokenSource(ctx, tokenData)
	httpClient := oauth2.NewClient(ctx, tokenSource)

	return &GeminiOAuthProvider{
		httpClient: httpClient,
		model:      model,
		projectID:  tokenData.ProjectID,
	}, nil
}

func (g *GeminiOAuthProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := codeAssistEndpoint + "/v1internal:streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setCodeAssistHeaders(httpReq)

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("code assist request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("code assist HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Collect all SSE chunks into a single response.
	var fullText strings.Builder
	var totalTokens int

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1 MB max line
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := parseSSEData(line)
		if !ok {
			continue
		}
		var chunk caResponseChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		text, tokens := extractCAChunk(chunk)
		fullText.WriteString(text)
		if tokens > 0 {
			totalTokens = tokens
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	return &ChatResponse{
		Content:    fullText.String(),
		TokensUsed: totalTokens,
	}, nil
}

func (g *GeminiOAuthProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := codeAssistEndpoint + "/v1internal:streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setCodeAssistHeaders(httpReq)

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("code assist stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("code assist stream HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			data, ok := parseSSEData(line)
			if !ok {
				continue
			}
			var chunk caResponseChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			text, _ := extractCAChunk(chunk)
			if text != "" {
				ch <- StreamChunk{Content: text}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err, Done: true}
			return
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

func (g *GeminiOAuthProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3")

	return ModelCapabilities{
		SupportsThinking: supportsThinking,
		ContextWindow:    1_000_000,
		SupportsStreaming: true,
		ProviderName:     "gemini-oauth",
	}
}

// --- Cloud Code Assist API types ---

type caRequest struct {
	Project string        `json:"project"`
	Model   string        `json:"model"`
	Request caRequestBody `json:"request"`
}

type caRequestBody struct {
	Contents          []caContent    `json:"contents"`
	SystemInstruction *caContent     `json:"systemInstruction,omitempty"`
	GenerationConfig  *caGenConfig   `json:"generationConfig,omitempty"`
}

type caContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []caPart `json:"parts"`
}

type caPart struct {
	Text string `json:"text"`
}

type caGenConfig struct {
	Temperature     *float32 `json:"temperature,omitempty"`
	MaxOutputTokens int32    `json:"maxOutputTokens,omitempty"`
	TopP            *float32 `json:"topP,omitempty"`
}

type caResponseChunk struct {
	Response *caResponseBody `json:"response,omitempty"`
}

type caResponseBody struct {
	Candidates    []caCandidate `json:"candidates,omitempty"`
	UsageMetadata *caUsage      `json:"usageMetadata,omitempty"`
}

type caCandidate struct {
	Content caContent `json:"content"`
}

type caUsage struct {
	TotalTokenCount int `json:"totalTokenCount"`
}

// --- Helpers ---

func (g *GeminiOAuthProvider) buildRequest(req ChatRequest) caRequest {
	config := buildConfig(req)

	var contents []caContent
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "model"
		}
		contents = append(contents, caContent{
			Role:  role,
			Parts: []caPart{{Text: msg.Content}},
		})
	}

	temperature := float32(config.temperature)
	topP := float32(config.topP)

	caReq := caRequest{
		Project: g.projectID,
		Model:   g.model,
		Request: caRequestBody{
			Contents: contents,
			GenerationConfig: &caGenConfig{
				Temperature:     &temperature,
				MaxOutputTokens: int32(config.maxTokens),
				TopP:            &topP,
			},
		},
	}

	if req.SystemPrompt != "" {
		caReq.Request.SystemInstruction = &caContent{
			Parts: []caPart{{Text: req.SystemPrompt}},
		}
	}

	return caReq
}

func setCodeAssistHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("X-Goog-Api-Client", "gl-node/22.17.0")
	req.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)
}

// parseSSEData extracts the JSON payload from an SSE data line.
// Handles both "data: {...}" and "data:{...}" formats.
func parseSSEData(line string) (string, bool) {
	if strings.HasPrefix(line, "data: ") {
		return strings.TrimPrefix(line, "data: "), true
	}
	if strings.HasPrefix(line, "data:") {
		return strings.TrimPrefix(line, "data:"), true
	}
	return "", false
}

func extractCAChunk(chunk caResponseChunk) (string, int) {
	if chunk.Response == nil || len(chunk.Response.Candidates) == 0 {
		return "", 0
	}
	var sb strings.Builder
	for _, part := range chunk.Response.Candidates[0].Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	tokens := 0
	if chunk.Response.UsageMetadata != nil {
		tokens = chunk.Response.UsageMetadata.TotalTokenCount
	}
	return sb.String(), tokens
}
