package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	log.Printf("[GeminiOAuth] Starting chat - Model: %s, Project: %s, Messages: %d",
		g.model, g.projectID, len(req.Messages))

	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	log.Printf("[GeminiOAuth] Request body size: %d bytes", len(body))

	url := codeAssistEndpoint + "/v1internal:streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setCodeAssistHeaders(httpReq)

	log.Printf("[GeminiOAuth] Sending request to %s", url)

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("code assist request: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[GeminiOAuth] Response status: %d", resp.StatusCode)
	log.Printf("[GeminiOAuth] Response headers: Content-Type=%s, Transfer-Encoding=%s",
		resp.Header.Get("Content-Type"), resp.Header.Get("Transfer-Encoding"))

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("code assist HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Try to peek at the first few bytes
	peekBytes := make([]byte, 100)
	n, peekErr := resp.Body.Read(peekBytes)
	if peekErr != nil && peekErr != io.EOF {
		log.Printf("[GeminiOAuth] Error peeking response body: %v", peekErr)
	} else {
		log.Printf("[GeminiOAuth] First %d bytes of response: %q", n, string(peekBytes[:n]))
	}

	// Create a new reader that includes the peeked bytes
	bodyReader := io.MultiReader(bytes.NewReader(peekBytes[:n]), resp.Body)

	// Collect all SSE chunks into a single response.
	var fullText strings.Builder
	var totalTokens int
	var lastFinishReason string
	chunkCount := 0

	log.Printf("[GeminiOAuth] Starting SSE stream processing")

	scanner := bufio.NewScanner(bodyReader)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1 MB max line
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		// Debug: log first few lines
		if lineCount <= 5 || (lineCount <= 15 && line != "") {
			truncated := line
			if len(truncated) > 200 {
				truncated = truncated[:200] + "..."
			}
			log.Printf("[GeminiOAuth] SSE Line %d: %q", lineCount, truncated)
		}

		data, ok := parseSSEData(line)
		if !ok {
			continue
		}

		log.Printf("[GeminiOAuth] Parsed SSE data (length: %d)", len(data))

		var chunk caResponseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			truncated := data
			if len(truncated) > 200 {
				truncated = truncated[:200] + "..."
			}
			log.Printf("[GeminiOAuth] JSON unmarshal error: %v, data: %s", err, truncated)
			continue
		}

		// Track finish reason from candidates
		if chunk.Response != nil && len(chunk.Response.Candidates) > 0 {
			candidate := chunk.Response.Candidates[0]
			if candidate.FinishReason != "" {
				lastFinishReason = candidate.FinishReason
				log.Printf("[GeminiOAuth] Finish reason: %s", lastFinishReason)
			}
		}

		text, tokens := extractCAChunk(chunk)
		log.Printf("[GeminiOAuth] Extracted text length: %d, tokens: %d", len(text), tokens)

		if text != "" {
			chunkCount++
			fullText.WriteString(text)
			log.Printf("[GeminiOAuth] Chunk %d: +%d chars (total: %d)",
				chunkCount, len(text), fullText.Len())
		}
		if tokens > 0 {
			totalTokens = tokens
		}
	}

	log.Printf("[GeminiOAuth] Total lines processed: %d", lineCount)

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	finalContent := fullText.String()
	log.Printf("[GeminiOAuth] Stream complete - Total: %d chunks, %d chars, %d tokens, finishReason: %s",
		chunkCount, len(finalContent), totalTokens, lastFinishReason)

	// Check for truncation
	if lastFinishReason == "MAX_TOKENS" {
		log.Printf("[GeminiOAuth] WARNING: Response was truncated due to MAX_TOKENS limit")
		log.Printf("[GeminiOAuth] Consider increasing max_tokens in persona config or using a shorter prompt")
	} else if lastFinishReason == "SAFETY" {
		return nil, fmt.Errorf("response blocked by safety filters")
	} else if lastFinishReason == "RECITATION" {
		return nil, fmt.Errorf("response blocked due to recitation concerns")
	} else if lastFinishReason == "BLOCKLIST" {
		return nil, fmt.Errorf("response blocked by content blocklist")
	} else if lastFinishReason != "" && lastFinishReason != "STOP" {
		log.Printf("[GeminiOAuth] WARNING: Unexpected finish reason: %s", lastFinishReason)
	}

	return &ChatResponse{
		Content:    finalContent,
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
	Content      caContent `json:"content"`
	FinishReason string    `json:"finishReason,omitempty"`
}

type caUsage struct {
	TotalTokenCount int `json:"totalTokenCount"`
}

// --- Helpers ---

func (g *GeminiOAuthProvider) buildRequest(req ChatRequest) caRequest {
	config := buildConfig(req)
	log.Printf("[GeminiOAuth] Generation config: temp=%.2f, maxTokens=%d, topP=%.2f",
		config.temperature, config.maxTokens, config.topP)

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

	genConfig := &caGenConfig{
		Temperature:     &temperature,
		MaxOutputTokens: int32(config.maxTokens),
		TopP:            &topP,
		// ThinkingConfig removed - not supported by Cloud Code Assist API
	}

	caReq := caRequest{
		Project: g.projectID,
		Model:   g.model,
		Request: caRequestBody{
			Contents:         contents,
			GenerationConfig: genConfig,
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
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
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
