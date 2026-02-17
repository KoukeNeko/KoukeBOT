package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

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

func (g *GeminiOAuthProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist request")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Collect all SSE chunks into a single response.
	var fullText strings.Builder
	var totalTokens int
	var lastFinishReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1 MB max line
	for scanner.Scan() {
		data, ok := parseSSEData(scanner.Text())
		if !ok {
			continue
		}

		var chunk caResponseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Track finish reason
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
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	finalContent := fullText.String()

	// Empty stream: 200 OK but no content received — likely a transient server issue.
	if finalContent == "" && totalTokens == 0 && lastFinishReason == "" {
		return nil, fmt.Errorf("empty stream: server returned 200 OK but no content")
	}

	// Check for abnormal finish reasons
	switch lastFinishReason {
	case "MAX_TOKENS":
		logOAuth.Warnf("response truncated (MAX_TOKENS) — consider increasing max_tokens")
	case "SAFETY":
		return nil, fmt.Errorf("response blocked by safety filters")
	case "RECITATION":
		return nil, fmt.Errorf("response blocked due to recitation concerns")
	case "BLOCKLIST":
		return nil, fmt.Errorf("response blocked by content blocklist")
	case "", "STOP":
		// Normal completion
	default:
		logOAuth.Warnf("unexpected finish reason: %s", lastFinishReason)
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

	resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist stream")
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

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
		// Empty stream: 200 OK but no content — likely transient server issue.
		if responseLen == 0 && lastFinishReason == "" {
			ch <- StreamChunk{
				Error: fmt.Errorf("empty stream: server returned 200 OK but no content"),
				Done:  true,
			}
			return
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
	}()

	return ch, nil
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

func (g *GeminiOAuthProvider) openStreamGenerateContent(ctx context.Context, body []byte, label string) (*http.Response, string, error) {
	candidates := g.currentEndpointCandidates()
	var lastErr error
	var lastErrEndpoint string

	for idx, endpoint := range candidates {
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
			if idx+1 < len(candidates) {
				logOAuth.Warnf("%s endpoint=%s failed (%v), retrying fallback endpoint", label, endpoint, err)
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

		if isEndpointFallbackEligibleStatus(resp.StatusCode) && idx+1 < len(candidates) {
			logOAuth.Warnf("%s endpoint=%s status=%d, retrying fallback endpoint", label, endpoint, resp.StatusCode)
			continue
		}
		return nil, endpoint, formattedErr
	}

	if lastErr != nil {
		return nil, lastErrEndpoint, lastErr
	}
	return nil, "", fmt.Errorf("%s: no endpoint candidates available", label)
}

func isEndpointFallbackEligibleStatus(statusCode int) bool {
	if statusCode >= 500 {
		return true
	}
	return statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooManyRequests // 429: try sandbox endpoint before failing
}

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
