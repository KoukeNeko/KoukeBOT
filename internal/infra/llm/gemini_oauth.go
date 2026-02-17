package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"golang.org/x/oauth2"
)

var logOAuth = logger.New("llm", logger.Yellow)

// GeminiOAuthProvider uses OAuth token + Cloud Code Assist API to access Gemini.
// The genai SDK requires an API key for BackendGeminiAPI, so we bypass it
// and make raw HTTP calls to cloudcode-pa.googleapis.com with Bearer auth.
type GeminiOAuthProvider struct {
	httpClient         *http.Client
	providerID         string
	model              string
	projectID          string
	endpointPolicy     string
	endpointCandidates []string
	endpointMu         sync.RWMutex
	effectiveEndpoint  string
	tools              []domain.Tool // Registered tools for function calling
}

// NewGeminiOAuthProvider creates a provider using OAuth credentials via Cloud Code Assist API.
func NewGeminiOAuthProvider(ctx context.Context, tokenData *OAuthTokenData, model string) (*GeminiOAuthProvider, error) {
	return NewGeminiOAuthProviderWithProvider(
		ctx,
		tokenData,
		ProviderGoogleGeminiCLI,
		model,
		EndpointPolicyPrimaryThenSandbox,
	)
}

// NewGeminiOAuthProviderWithProvider creates a provider with explicit provider identity
// and endpoint policy/candidate configuration.
func NewGeminiOAuthProviderWithProvider(
	ctx context.Context,
	tokenData *OAuthTokenData,
	providerID string,
	model string,
	endpointPolicy string,
) (*GeminiOAuthProvider, error) {
	tokenSource := NewTokenSource(ctx, tokenData)
	httpClient := oauth2.NewClient(ctx, tokenSource)
	pid := NormalizeGoogleProviderID(providerID)
	spec := googleProviderSpec(pid)
	candidates := resolveOAuthEndpointCandidates(spec.EndpointCandidates, endpointPolicy)
	normalizedModel := NormalizeGoogleModelAlias(model)
	if normalizedModel == "" {
		normalizedModel = spec.DefaultModel
	}

	return &GeminiOAuthProvider{
		httpClient:         httpClient,
		providerID:         pid,
		model:              normalizedModel,
		projectID:          tokenData.ProjectID,
		endpointPolicy:     ResolveEndpointPolicy(endpointPolicy),
		endpointCandidates: candidates,
		effectiveEndpoint:  candidates[0],
	}, nil
}

// WithModel returns a shallow clone of this provider configured for a different model.
// The HTTP client (and thus OAuth credentials) is shared.
func (g *GeminiOAuthProvider) WithModel(model string) Provider {
	g.endpointMu.RLock()
	effective := g.effectiveEndpoint
	candidates := append([]string(nil), g.endpointCandidates...)
	g.endpointMu.RUnlock()

	return &GeminiOAuthProvider{
		httpClient:         g.httpClient,
		providerID:         g.providerID,
		model:              NormalizeGoogleModelAlias(model),
		projectID:          g.projectID,
		endpointPolicy:     g.endpointPolicy,
		endpointCandidates: candidates,
		effectiveEndpoint:  effective,
		tools:              append([]domain.Tool(nil), g.tools...),
	}
}

func resolveOAuthEndpointCandidates(candidates []string, endpointPolicy string) []string {
	policy := ResolveEndpointPolicy(endpointPolicy)
	resolved := make([]string, 0, len(candidates))
	for _, endpoint := range candidates {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		resolved = append(resolved, endpoint)
	}
	if len(resolved) == 0 {
		resolved = []string{codeAssistPrimaryEndpoint, codeAssistSandboxEndpoint}
	}
	if policy != EndpointPolicyPrimaryThenSandbox {
		return resolved
	}
	return resolved
}

// Empty stream retry constants (aligned with pi-ai).
const (
	maxEmptyStreamRetries   = 2
	emptyStreamBaseDelay    = 500 * time.Millisecond
)

func (g *GeminiOAuthProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Retry loop for empty streams (pi-ai: MAX_EMPTY_STREAM_RETRIES = 2).
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist request")
		if err != nil {
			return nil, err
		}

		result, emptyStream := g.consumeSSEResponse(resp)
		if !emptyStream {
			return result.chatResponse, result.err
		}

		resp.Body.Close()
		if emptyRetry < maxEmptyStreamRetries {
			delay := emptyStreamBaseDelay * time.Duration(1<<emptyRetry)
			logOAuth.Warnf("empty stream (attempt %d/%d), retrying in %s",
				emptyRetry+1, maxEmptyStreamRetries+1, delay)
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
	}

	return nil, fmt.Errorf("empty stream: server returned 200 OK but no content after %d retries", maxEmptyStreamRetries+1)
}

// chatResult holds the result of consuming an SSE response for Chat().
type chatResult struct {
	chatResponse *ChatResponse
	err          error
}

// consumeSSEResponse reads an SSE stream and returns the parsed result.
// Returns (result, true) if the stream was empty (should retry).
func (g *GeminiOAuthProvider) consumeSSEResponse(resp *http.Response) (chatResult, bool) {
	defer resp.Body.Close()

	var fullText strings.Builder
	var totalTokens int
	var lastFinishReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		data, ok := parseSSEData(scanner.Text())
		if !ok {
			continue
		}
		var chunk caResponseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Response != nil && len(chunk.Response.Candidates) > 0 {
			if fr := chunk.Response.Candidates[0].FinishReason; fr != "" {
				lastFinishReason = fr
			}
		}
		parsed := extractCAChunkFull(chunk)
		fullText.WriteString(parsed.text)
		if parsed.tokens > 0 {
			totalTokens = parsed.tokens
		}
	}

	if err := scanner.Err(); err != nil {
		return chatResult{err: fmt.Errorf("read SSE stream: %w", err)}, false
	}

	finalContent := fullText.String()

	// Empty stream detection — signal caller to retry.
	if finalContent == "" && totalTokens == 0 && lastFinishReason == "" {
		return chatResult{}, true
	}

	switch lastFinishReason {
	case "MAX_TOKENS":
		logOAuth.Warnf("response truncated (MAX_TOKENS) — consider increasing max_tokens")
	case "SAFETY":
		return chatResult{err: fmt.Errorf("response blocked by safety filters")}, false
	case "RECITATION":
		return chatResult{err: fmt.Errorf("response blocked due to recitation concerns")}, false
	case "BLOCKLIST":
		return chatResult{err: fmt.Errorf("response blocked by content blocklist")}, false
	case "", "STOP":
		// Normal completion
	default:
		logOAuth.Warnf("unexpected finish reason: %s", lastFinishReason)
	}

	return chatResult{chatResponse: &ChatResponse{
		Content:    finalContent,
		TokensUsed: totalTokens,
	}}, false
}

func (g *GeminiOAuthProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Retry empty streams at HTTP level before handing off to the streaming goroutine.
	var resp *http.Response
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		var openErr error
		resp, _, openErr = g.openStreamGenerateContent(ctx, body, "code assist stream")
		if openErr != nil {
			return nil, openErr
		}

		// Peek: try to read until we get the first data line or EOF.
		// If we can confirm it's a non-empty stream, break and stream it.
		// For streaming we can't fully consume, so we stream directly and
		// handle empty stream in the goroutine with retry signaling.
		break
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		g.streamSSEWithEmptyRetry(ctx, body, resp, ch)
	}()

	return ch, nil
}

// streamSSEWithEmptyRetry reads an SSE stream and retries on empty streams.
func (g *GeminiOAuthProvider) streamSSEWithEmptyRetry(ctx context.Context, body []byte, resp *http.Response, ch chan<- StreamChunk) {
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		emptyStream := g.consumeStreamSSE(resp, ch)
		resp.Body.Close()

		if !emptyStream {
			return // Normal completion or error already sent to channel
		}

		// Empty stream — retry if we have attempts left.
		if emptyRetry >= maxEmptyStreamRetries {
			ch <- StreamChunk{
				Error: fmt.Errorf("empty stream: server returned 200 OK but no content after %d retries", maxEmptyStreamRetries+1),
				Done:  true,
			}
			return
		}

		delay := emptyStreamBaseDelay * time.Duration(1<<emptyRetry)
		logOAuth.Warnf("empty stream (attempt %d/%d), retrying in %s",
			emptyRetry+1, maxEmptyStreamRetries+1, delay)
		if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
			ch <- StreamChunk{Error: sleepErr, Done: true}
			return
		}

		// Re-open the stream for retry.
		var openErr error
		resp, _, openErr = g.openStreamGenerateContent(ctx, body, "code assist stream")
		if openErr != nil {
			ch <- StreamChunk{Error: openErr, Done: true}
			return
		}
	}
}

// consumeStreamSSE reads an SSE stream into the channel.
// Returns true if the stream was empty (caller should retry).
func (g *GeminiOAuthProvider) consumeStreamSSE(resp *http.Response, ch chan<- StreamChunk) bool {
	var lastUsage *caUsage
	var lastFinishReason string
	var responseLen int
	var responsePreview string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		data, ok := parseSSEData(scanner.Text())
		if !ok {
			continue
		}
		var chunk caResponseChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		parsed := extractCAChunkFull(chunk)
		if parsed.usage != nil {
			lastUsage = parsed.usage
		}
		if parsed.finishReason != "" {
			lastFinishReason = parsed.finishReason
		}

		// Emit function calls to trigger the tool calling loop
		if len(parsed.toolCalls) > 0 {
			logOAuth.Logf("functionCall: %s", formatToolCallNames(parsed.toolCalls))
			logUsage(lastUsage)
			tokens := 0
			if lastUsage != nil {
				tokens = lastUsage.TotalTokenCount
			}
			ch <- StreamChunk{ToolCalls: parsed.toolCalls, Done: true, TokensUsed: tokens}
			return false
		}

		if parsed.text != "" {
			responseLen += len(parsed.text)
			if len(responsePreview) < 80 {
				responsePreview += parsed.text
			}
			ch <- StreamChunk{Content: parsed.text}
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Error: err, Done: true}
		return false
	}

	// Empty stream: signal caller to retry.
	if responseLen == 0 && lastFinishReason == "" {
		return true
	}

	logCompletion(lastUsage, responsePreview, responseLen)
	if lastFinishReason == "MAX_TOKENS" {
		logOAuth.Warnf("stream response truncated (MAX_TOKENS) — consider increasing max_tokens")
	}
	tokens := 0
	if lastUsage != nil {
		tokens = lastUsage.TotalTokenCount
	}
	ch <- StreamChunk{Done: true, TokensUsed: tokens, FinishReason: lastFinishReason}
	return false
}

func (g *GeminiOAuthProvider) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan StreamChunk, error) {
	body, err := json.Marshal(g.buildMultimodalRequest(systemPrompt, messages, generation))
	if err != nil {
		return nil, fmt.Errorf("marshal multimodal request: %w", err)
	}

	resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist multimodal stream")
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var lastUsage *caUsage
		var responseLen int
		var responsePreview string
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for scanner.Scan() {
			data, ok := parseSSEData(scanner.Text())
			if !ok {
				continue
			}
			var chunk caResponseChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			parsed := extractCAChunkFull(chunk)
			if parsed.usage != nil {
				lastUsage = parsed.usage
			}

			// Emit function calls to trigger the tool calling loop
			if len(parsed.toolCalls) > 0 {
				logOAuth.Logf("functionCall: %s", formatToolCallNames(parsed.toolCalls))
				logUsage(lastUsage)
				mmTokens := 0
				if lastUsage != nil {
					mmTokens = lastUsage.TotalTokenCount
				}
				ch <- StreamChunk{ToolCalls: parsed.toolCalls, Done: true, TokensUsed: mmTokens}
				return
			}

			if parsed.text != "" {
				responseLen += len(parsed.text)
				if len(responsePreview) < 80 {
					responsePreview += parsed.text
				}
				ch <- StreamChunk{Content: parsed.text}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err, Done: true}
			return
		}
		logCompletion(lastUsage, responsePreview, responseLen)
		tokens := 0
		if lastUsage != nil {
			tokens = lastUsage.TotalTokenCount
		}
		ch <- StreamChunk{Done: true, TokensUsed: tokens}
	}()

	return ch, nil
}

func (g *GeminiOAuthProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3") ||
		strings.Contains(g.model, "thinking")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     1_000_000,
		MaxOutputTokens:   resolveMaxTokensForModel(g.model, defaultMaxOutputTokens),
		SupportsStreaming: true,
		ProviderName:      fmt.Sprintf("gemini-oauth/%s", g.providerID),
		Model:             g.model,
	}
}

// EffectiveEndpoint returns the endpoint of the last successful OAuth request.
func (g *GeminiOAuthProvider) EffectiveEndpoint() string {
	g.endpointMu.RLock()
	defer g.endpointMu.RUnlock()
	return g.effectiveEndpoint
}

// RegisterTools registers tools for function calling (OAuth provider).
// Tools will be sent as functionDeclarations in every API request.
func (g *GeminiOAuthProvider) RegisterTools(tools []domain.Tool) {
	g.tools = tools
}

// --- Cloud Code Assist API types ---

type caRequest struct {
	Project string        `json:"project"`
	Model   string        `json:"model"`
	Request caRequestBody `json:"request"`
}

type caRequestBody struct {
	Contents          []caContent  `json:"contents"`
	SystemInstruction *caContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *caGenConfig `json:"generationConfig,omitempty"`
	Tools             []caToolDecl `json:"tools,omitempty"`
}

// caToolDecl wraps function declarations for the Cloud Code Assist API.
type caToolDecl struct {
	FunctionDeclarations []caFunctionDecl `json:"functionDeclarations"`
}

// caFunctionDecl describes a single function the LLM can call.
type caFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type caContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []caPart `json:"parts"`
}

type caPart struct {
	Text         string          `json:"text,omitempty"`
	InlineData   *caInlineData   `json:"inlineData,omitempty"`
	FunctionCall *caFunctionCall `json:"functionCall,omitempty"`
}

// caFunctionCall represents a function call returned by the LLM.
type caFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type caInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
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
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Helpers ---

func (g *GeminiOAuthProvider) buildRequest(req ChatRequest) caRequest {
	config := buildConfig(req, g.model)

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

	toolsCount := len(g.tools)
	if req.ToolMode == ToolModeDisabled {
		toolsCount = 0
	}

	// Include tool declarations for function calling unless explicitly disabled.
	if req.ToolMode != ToolModeDisabled && len(g.tools) > 0 {
		caReq.Request.Tools = convertToolsForCA(g.tools)
	}

	logOAuth.Logf("request: messages=%d, tools=%d, temp=%.1f, maxTokens=%d",
		len(req.Messages), toolsCount, config.temperature, config.maxTokens)

	return caReq
}

func (g *GeminiOAuthProvider) buildMultimodalRequest(systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) caRequest {
	config := buildGenerationConfig(generation, g.model)

	var contents []caContent
	for _, msg := range messages {
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "model"
		}

		var parts []caPart
		for _, p := range msg.Parts {
			if p.Text != "" {
				parts = append(parts, caPart{Text: p.Text})
			}
			if p.ImageData != nil {
				// Encode image data as base64 for inline_data
				encodedData := base64.StdEncoding.EncodeToString(p.ImageData.Data)
				parts = append(parts, caPart{
					InlineData: &caInlineData{
						MimeType: p.ImageData.MimeType,
						Data:     encodedData,
					},
				})
			}
		}

		if len(parts) > 0 {
			contents = append(contents, caContent{
				Role:  role,
				Parts: parts,
			})
		}
	}

	temperature := float32(config.temperature)
	topP := float32(config.topP)

	genConfig := &caGenConfig{
		Temperature:     &temperature,
		MaxOutputTokens: int32(config.maxTokens),
		TopP:            &topP,
	}

	caReq := caRequest{
		Project: g.projectID,
		Model:   g.model,
		Request: caRequestBody{
			Contents:         contents,
			GenerationConfig: genConfig,
		},
	}

	if systemPrompt != "" {
		caReq.Request.SystemInstruction = &caContent{
			Parts: []caPart{{Text: systemPrompt}},
		}
	}

	// Include tool declarations for function calling
	if len(g.tools) > 0 {
		caReq.Request.Tools = convertToolsForCA(g.tools)
	}

	// Count images and total size
	imageCount := 0
	totalImageBytes := 0
	for _, msg := range messages {
		for _, p := range msg.Parts {
			if p.ImageData != nil {
				imageCount++
				totalImageBytes += len(p.ImageData.Data)
			}
		}
	}

	if imageCount > 0 {
		logOAuth.Logf("multimodal request: messages=%d, images=%d (%.1fMB), tools=%d, temp=%.1f, maxTokens=%d",
			len(messages), imageCount, float64(totalImageBytes)/(1024*1024), len(g.tools), config.temperature, config.maxTokens)
	} else {
		logOAuth.Logf("multimodal request: messages=%d, tools=%d, temp=%.1f, maxTokens=%d",
			len(messages), len(g.tools), config.temperature, config.maxTokens)
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

// HTTP retry constants (aligned with pi-ai's google-gemini-cli provider).
const (
	httpMaxRetries        = 3             // Total retry attempts (4 attempts including initial)
	httpBaseDelay         = 1 * time.Second
	httpMaxServerDelay    = 60 * time.Second // If server requests longer, return error to pool layer
)

func (g *GeminiOAuthProvider) openStreamGenerateContent(ctx context.Context, body []byte, label string) (*http.Response, string, error) {
	candidates := g.currentEndpointCandidates()
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("%s: no endpoint candidates available", label)
	}

	var lastErr error
	var lastErrEndpoint string

	for attempt := 0; attempt <= httpMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, lastErrEndpoint, ctx.Err()
		}

		// Rotate endpoint on each attempt (pi-ai: endpoints[min(attempt, len-1)]).
		endpointIdx := attempt
		if endpointIdx >= len(candidates) {
			endpointIdx = len(candidates) - 1
		}
		endpoint := candidates[endpointIdx]

		reqURL := strings.TrimRight(endpoint, "/") + "/v1internal:streamGenerateContent?alt=sse"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
		if err != nil {
			return nil, endpoint, fmt.Errorf("create request: %w", err)
		}
		setCodeAssistHeaders(httpReq)

		resp, err := g.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", label, err)
			lastErrEndpoint = endpoint
			if attempt < httpMaxRetries {
				delay := httpBackoffDelay(attempt)
				logOAuth.Warnf("%s endpoint=%s attempt=%d failed (%v), retrying in %s",
					label, endpoint, attempt+1, err, delay)
				if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
					return nil, endpoint, sleepErr
				}
				continue
			}
			return nil, endpoint, lastErr
		}

		if resp.StatusCode == http.StatusOK {
			g.setEffectiveEndpoint(endpoint)
			return resp, endpoint, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Embed rate-limit headers into the error message so the upper-layer
		// parseResetDelay() can extract the server's recommended wait time.
		headerInfo := extractRateLimitHeaders(resp)
		formattedErr := fmt.Errorf("%s HTTP %d:%s %s", label, resp.StatusCode, headerInfo, string(respBody))
		lastErr = formattedErr
		lastErrEndpoint = endpoint

		// Check if this error is retryable at the HTTP level.
		if attempt < httpMaxRetries && isHTTPRetryable(resp.StatusCode, string(respBody)) {
			// Extract server-recommended delay from headers and body.
			serverDelay := extractHTTPRetryDelay(string(respBody), resp)

			// If server requests more than maxServerDelay, stop retrying at HTTP
			// level and let the pool layer handle it with account rotation.
			if serverDelay > httpMaxServerDelay {
				logOAuth.Warnf("%s endpoint=%s status=%d: server requested %s delay (max %s), deferring to pool",
					label, endpoint, resp.StatusCode, serverDelay, httpMaxServerDelay)
				return nil, endpoint, formattedErr
			}

			delay := httpBackoffDelay(attempt)
			if serverDelay > delay {
				delay = serverDelay
			}
			logOAuth.Warnf("%s endpoint=%s status=%d attempt=%d, retrying in %s",
				label, endpoint, resp.StatusCode, attempt+1, delay)
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, endpoint, sleepErr
			}
			continue
		}

		return nil, endpoint, formattedErr
	}

	if lastErr != nil {
		return nil, lastErrEndpoint, lastErr
	}
	return nil, "", fmt.Errorf("%s: no endpoint candidates available", label)
}

// isHTTPRetryable checks if an HTTP error should be retried at the transport level.
// Aligned with pi-ai's isRetryableError: 429, 5xx, and retryable body patterns.
func isHTTPRetryable(statusCode int, body string) bool {
	if statusCode == http.StatusTooManyRequests ||
		statusCode >= 500 {
		return true
	}
	return httpRetryableBodyRe.MatchString(body)
}

var httpRetryableBodyRe = regexp.MustCompile(`(?i)resource.?exhausted|rate.?limit|overloaded|service.?unavailable|other.?side.?closed`)

// httpBackoffDelay returns exponential backoff: 1s, 2s, 4s, ...
func httpBackoffDelay(attempt int) time.Duration {
	return httpBaseDelay * time.Duration(math.Pow(2, float64(attempt)))
}

// sleepWithContext sleeps for the given duration, returning early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// extractHTTPRetryDelay extracts the server-recommended retry delay from
// HTTP response headers and body text. Returns 0 if no delay is found.
// Aligned with pi-ai's extractRetryDelay.
func extractHTTPRetryDelay(errorText string, resp *http.Response) time.Duration {
	if resp != nil {
		// 1. Retry-After header (seconds or HTTP-date)
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs)*time.Second + time.Second
			}
		}
		// 2. X-RateLimit-Reset-After header
		if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs)*time.Second + time.Second
			}
		}
	}
	// 3. Body: "reset after 18h31m10s" / "reset after 39s"
	if m := httpResetAfterRe.FindStringSubmatch(errorText); len(m) >= 4 {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		total := time.Duration(h)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second))
		if total > 0 {
			return total + time.Second
		}
	}
	// 4. Body: "Please retry in Xs" or "Please retry in Xms"
	if m := httpRetryInRe.FindStringSubmatch(errorText); len(m) >= 3 {
		val, _ := strconv.ParseFloat(m[1], 64)
		if val > 0 {
			if strings.ToLower(m[2]) == "ms" {
				return time.Duration(val*float64(time.Millisecond)) + time.Second
			}
			return time.Duration(val*float64(time.Second)) + time.Second
		}
	}
	// 5. Body JSON: "retryDelay": "34.074824224s"
	if m := httpRetryDelayJSONRe.FindStringSubmatch(errorText); len(m) >= 3 {
		val, _ := strconv.ParseFloat(m[1], 64)
		if val > 0 {
			if strings.ToLower(m[2]) == "ms" {
				return time.Duration(val*float64(time.Millisecond)) + time.Second
			}
			return time.Duration(val*float64(time.Second)) + time.Second
		}
	}
	return 0
}

var (
	httpResetAfterRe    = regexp.MustCompile(`(?i)reset after (?:(\d+)h)?(?:(\d+)m)?(\d+(?:\.\d+)?)s`)
	httpRetryInRe       = regexp.MustCompile(`(?i)Please retry in ([0-9.]+)(ms|s)`)
	httpRetryDelayJSONRe = regexp.MustCompile(`(?i)"retryDelay"\s*:\s*"([0-9.]+)(ms|s)"`)
)

func (g *GeminiOAuthProvider) currentEndpointCandidates() []string {
	g.endpointMu.RLock()
	defer g.endpointMu.RUnlock()
	if len(g.endpointCandidates) == 0 {
		return []string{codeAssistPrimaryEndpoint, codeAssistSandboxEndpoint}
	}
	return append([]string(nil), g.endpointCandidates...)
}

func (g *GeminiOAuthProvider) setEffectiveEndpoint(endpoint string) {
	g.endpointMu.Lock()
	defer g.endpointMu.Unlock()
	g.effectiveEndpoint = endpoint
}

// extractRateLimitHeaders builds a bracketed string of rate-limit headers from
// the HTTP response. The format is designed to be parsed by parseResetDelay()
// in the provider pool layer (e.g., "[retry-after:39] [x-ratelimit-reset-after:45]").
func extractRateLimitHeaders(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	var sb strings.Builder
	if v := resp.Header.Get("Retry-After"); v != "" {
		sb.WriteString(fmt.Sprintf(" [retry-after:%s]", v))
	}
	if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
		sb.WriteString(fmt.Sprintf(" [x-ratelimit-reset-after:%s]", v))
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		sb.WriteString(fmt.Sprintf(" [x-ratelimit-reset:%s]", v))
	}
	return sb.String()
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

// caChunkResult holds parsed data from a single SSE chunk.
type caChunkResult struct {
	text         string
	tokens       int
	usage        *caUsage // full usage metadata (last chunk only)
	toolCalls    []domain.ToolCall
	finishReason string
}

func extractCAChunkFull(chunk caResponseChunk) caChunkResult {
	result := caChunkResult{}
	if chunk.Response == nil || len(chunk.Response.Candidates) == 0 {
		return result
	}

	candidate := chunk.Response.Candidates[0]
	if candidate.FinishReason != "" {
		result.finishReason = candidate.FinishReason
	}

	var sb strings.Builder
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			result.toolCalls = append(result.toolCalls, domain.ToolCall{
				ID:    part.FunctionCall.Name,
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		}
	}
	result.text = sb.String()

	if chunk.Response.UsageMetadata != nil {
		result.tokens = chunk.Response.UsageMetadata.TotalTokenCount
		result.usage = chunk.Response.UsageMetadata
	}
	return result
}

// logUsage logs token usage summary from the final SSE chunk.
func logUsage(usage *caUsage) {
	if usage == nil {
		return
	}
	logOAuth.Logf("tokens: prompt=%d, response=%d, total=%d",
		usage.PromptTokenCount, usage.CandidatesTokenCount, usage.TotalTokenCount)
}

// logCompletion logs token usage and a preview of the response.
func logCompletion(usage *caUsage, preview string, totalLen int) {
	logUsage(usage)
	// Clean preview: collapse newlines to spaces, truncate
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.ReplaceAll(preview, "\r", "")
	if len(preview) > 80 {
		preview = preview[:80]
	}
	logOAuth.Logf("response: %d chars | %s...", totalLen, strings.TrimSpace(preview))
}

// formatToolCallNames returns a compact string of tool call names.
func formatToolCallNames(calls []domain.ToolCall) string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// --- Schema cleaning for Gemini compatibility (OpenClaw pattern) ---

// geminiUnsupportedSchemaKeywords lists JSON Schema keywords that
// Cloud Code Assist API rejects. Based on OpenClaw's cleanSchemaForGemini.
var geminiUnsupportedSchemaKeywords = map[string]bool{
	"patternProperties":    true,
	"additionalProperties": true,
	"$schema":              true,
	"$id":                  true,
	"$ref":                 true,
	"$defs":                true,
	"definitions":          true,
	"examples":             true,
	"minLength":            true,
	"maxLength":            true,
	"minimum":              true,
	"maximum":              true,
	"multipleOf":           true,
	"pattern":              true,
	"format":               true,
	"minItems":             true,
	"maxItems":             true,
	"uniqueItems":          true,
	"minProperties":        true,
	"maxProperties":        true,
}

// cleanSchemaForGemini recursively removes unsupported JSON Schema keywords.
func cleanSchemaForGemini(schema map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{}, len(schema))
	for key, val := range schema {
		if geminiUnsupportedSchemaKeywords[key] {
			continue
		}
		switch v := val.(type) {
		case map[string]interface{}:
			cleaned[key] = cleanSchemaForGemini(v)
		default:
			cleaned[key] = val
		}
	}
	return cleaned
}

// convertToolsForCA converts domain.Tool slice to Cloud Code Assist API format
// with Gemini-specific schema cleaning applied.
func convertToolsForCA(tools []domain.Tool) []caToolDecl {
	if len(tools) == 0 {
		return nil
	}

	declarations := make([]caFunctionDecl, 0, len(tools))
	for _, tool := range tools {
		cleanedSchema := cleanSchemaForGemini(tool.InputSchema())
		declarations = append(declarations, caFunctionDecl{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  cleanedSchema,
		})
	}

	return []caToolDecl{{FunctionDeclarations: declarations}}
}
