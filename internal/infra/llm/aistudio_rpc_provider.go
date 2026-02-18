package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const (
	aiStudioRPCGenerateContentPath = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateContent"
	aiStudioRPCCountTokensPath     = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/CountTokens"
	aiStudioRPCGenerateAccessToken = "/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService/GenerateAccessToken"
	aiStudioRPCXUserAgent          = "grpc-web-javascript/0.1"
	aiStudioAccessTokenTTL         = 55 * time.Minute
	aiStudioAccessTokenRefreshSkew = 10 * time.Minute
)

// AIStudioRPCProvider is a best-effort, non-official adapter that calls the
// same MakerSuite RPC endpoint used by AI Studio Web.
type AIStudioRPCProvider struct {
	httpClient *http.Client
	session    AIStudioSession
	model      string

	mu    sync.RWMutex
	tools []domain.Tool

	authMu            sync.Mutex
	accessToken       string
	accessTokenExpiry time.Time
}

// NewAIStudioRPCProvider creates an AI Studio RPC provider from a persisted session.
func NewAIStudioRPCProvider(session *AIStudioSession, model string) (*AIStudioRPCProvider, error) {
	normalized := normalizeAIStudioSession(session)
	if err := ValidateAIStudioSession(normalized); err != nil {
		return nil, err
	}

	normalizedModel := NormalizeGoogleModelAlias(model)
	if normalizedModel == "" {
		normalizedModel = googleProviderSpec(ProviderGoogleGeminiCLI).DefaultModel
	}

	return &AIStudioRPCProvider{
		httpClient: &http.Client{Timeout: 70 * time.Second},
		session:    *normalized,
		model:      normalizedModel,
	}, nil
}

// VerifyAIStudioSession performs a lightweight CountTokens probe.
func VerifyAIStudioSession(ctx context.Context, session *AIStudioSession, model string) error {
	provider, err := NewAIStudioRPCProvider(session, model)
	if err != nil {
		return err
	}
	return provider.countTokens(ctx, "ping")
}

// WithModel returns a shallow clone configured for another model.
func (g *AIStudioRPCProvider) WithModel(model string) Provider {
	g.mu.RLock()
	tools := append([]domain.Tool(nil), g.tools...)
	g.mu.RUnlock()
	g.authMu.Lock()
	accessToken := g.accessToken
	accessTokenExpiry := g.accessTokenExpiry
	g.authMu.Unlock()

	normalizedModel := NormalizeGoogleModelAlias(model)
	if normalizedModel == "" {
		normalizedModel = g.model
	}

	return &AIStudioRPCProvider{
		httpClient:        g.httpClient,
		session:           g.session,
		model:             normalizedModel,
		tools:             tools,
		accessToken:       accessToken,
		accessTokenExpiry: accessTokenExpiry,
	}
}

func (g *AIStudioRPCProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     1_000_000,
		MaxOutputTokens:   resolveMaxTokensForModel(g.model, defaultMaxOutputTokens),
		SupportsStreaming: true,
		ProviderName:      "aistudio-rpc",
		Model:             g.model,
	}
}

// EffectiveEndpoint returns the endpoint used by this provider.
func (g *AIStudioRPCProvider) EffectiveEndpoint() string {
	return g.session.Endpoint
}

func (g *AIStudioRPCProvider) RegisterTools(tools []domain.Tool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tools = append([]domain.Tool(nil), tools...)
}

func (g *AIStudioRPCProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	payload := g.buildGenerateContentPayload(req)
	raw, err := g.callRPC(ctx, aiStudioRPCGenerateContentPath, payload)
	if err != nil {
		return nil, err
	}

	content, err := parseAIStudioFinalAnswer(raw)
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content:      content,
		FinishReason: "STOP",
	}, nil
}

func (g *AIStudioRPCProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	resp, err := g.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		for _, chunk := range splitAIStudioText(resp.Content, 140) {
			ch <- StreamChunk{Content: chunk}
		}
		ch <- StreamChunk{Done: true, FinishReason: "STOP"}
	}()
	return ch, nil
}

func (g *AIStudioRPCProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("aistudio rpc multimodal is not supported in this fallback provider")
}

func (g *AIStudioRPCProvider) countTokens(ctx context.Context, text string) error {
	payload := g.buildCountTokensPayload(text)
	_, err := g.callRPC(ctx, aiStudioRPCCountTokensPath, payload)
	return err
}

func (g *AIStudioRPCProvider) buildGenerateContentPayload(req ChatRequest) []any {
	messages := make([]domain.Message, 0, len(req.Messages)+1)
	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		// AI Studio RPC payload has no explicit system field in the observed schema.
		// Prefix system instructions as a synthetic user turn.
		messages = append(messages, domain.Message{
			Role:    domain.RoleUser,
			Content: "[System]\n" + system,
		})
	}
	messages = append(messages, req.Messages...)

	history := aiStudioHistoryFromMessages(messages)
	maxTokens := resolveMaxTokensForModel(g.model, defaultMaxOutputTokens)

	topP := req.Generation.TopP
	if topP <= 0 {
		topP = 0.95
	}

	return []any{
		"models/" + NormalizeGoogleModelAlias(g.model), // model
		history, // conversation history
		nil,     // safety/rule config placeholder
		[]any{
			nil,
			nil,
			nil,
			maxTokens,
			1,
			topP,
			64,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			1,
		},
		"",
		nil,
		[]any{[]any{nil, nil, nil, []any{}}},
		nil,
		nil,
		nil,
		1,
		nil,
		nil,
		[]any{[]any{nil, nil, "UTC"}},
	}
}

func (g *AIStudioRPCProvider) buildCountTokensPayload(text string) []any {
	messages := aiStudioHistoryFromMessages([]domain.Message{
		{Role: domain.RoleUser, Content: text},
	})
	return []any{
		"models/" + NormalizeGoogleModelAlias(g.model),
		messages,
	}
}

func aiStudioHistoryFromMessages(messages []domain.Message) []any {
	history := make([]any, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "model"
		}
		history = append(history, []any{
			[]any{
				[]any{nil, content},
			},
			role,
		})
	}
	if len(history) == 0 {
		history = append(history, []any{
			[]any{
				[]any{nil, "."},
			},
			"user",
		})
	}
	return history
}

func (g *AIStudioRPCProvider) callRPC(ctx context.Context, path string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal AI Studio payload: %w", err)
	}

	authHeader, authErr := g.resolveRPCAuthHeader(ctx, path, false)
	statusCode, respBody, err := g.doRPCRequest(ctx, path, body, authHeader)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusUnauthorized && path != aiStudioRPCGenerateAccessToken {
		refreshedAuth, refreshErr := g.resolveRPCAuthHeader(ctx, path, true)
		if refreshErr == nil && strings.TrimSpace(refreshedAuth) != "" && refreshedAuth != authHeader {
			statusCode, respBody, err = g.doRPCRequest(ctx, path, body, refreshedAuth)
			if err != nil {
				return nil, err
			}
			authHeader = refreshedAuth
		} else if authErr == nil {
			authErr = refreshErr
		}
	}

	if statusCode != http.StatusOK {
		msg := summarizeAIStudioBody(respBody, 500)
		if authErr != nil {
			msg += " (auth refresh unavailable)"
		}
		return nil, fmt.Errorf(
			"AI Studio RPC %s HTTP %d: %s",
			path,
			statusCode,
			msg,
		)
	}
	return respBody, nil
}

func (g *AIStudioRPCProvider) resolveRPCAuthHeader(ctx context.Context, path string, forceRefresh bool) (string, error) {
	staticAuth := strings.TrimSpace(g.session.Authorization)
	if path == aiStudioRPCGenerateAccessToken {
		return staticAuth, nil
	}

	token, err := g.ensureAccessToken(ctx, forceRefresh)
	if token != "" {
		return "Bearer " + token, nil
	}
	if staticAuth != "" {
		return staticAuth, err
	}
	return "", err
}

func (g *AIStudioRPCProvider) ensureAccessToken(ctx context.Context, forceRefresh bool) (string, error) {
	if strings.TrimSpace(g.session.Cookie) == "" {
		return "", fmt.Errorf("AI Studio session missing cookie for automatic token refresh")
	}

	g.authMu.Lock()
	defer g.authMu.Unlock()

	if !forceRefresh {
		token := strings.TrimSpace(g.accessToken)
		if token != "" {
			refreshAt := g.accessTokenExpiry.Add(-aiStudioAccessTokenRefreshSkew)
			if g.accessTokenExpiry.IsZero() || time.Now().Before(refreshAt) {
				return token, nil
			}
		}
	}

	token, err := g.generateAccessTokenLocked(ctx)
	if err != nil {
		return "", err
	}
	g.accessToken = token
	g.accessTokenExpiry = time.Now().Add(aiStudioAccessTokenTTL)
	return g.accessToken, nil
}

func (g *AIStudioRPCProvider) generateAccessTokenLocked(ctx context.Context) (string, error) {
	body, err := json.Marshal([]any{"users/me"})
	if err != nil {
		return "", fmt.Errorf("marshal AI Studio access token payload: %w", err)
	}

	statusCode, respBody, err := g.doRPCRequest(ctx, aiStudioRPCGenerateAccessToken, body, strings.TrimSpace(g.session.Authorization))
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf(
			"AI Studio RPC %s HTTP %d: %s",
			aiStudioRPCGenerateAccessToken,
			statusCode,
			summarizeAIStudioBody(respBody, 300),
		)
	}

	token, err := parseAIStudioAccessToken(respBody)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (g *AIStudioRPCProvider) doRPCRequest(ctx context.Context, path string, body []byte, authHeader string) (int, []byte, error) {
	url := strings.TrimRight(g.session.Endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("create AI Studio request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json+protobuf")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", g.session.Origin)
	req.Header.Set("Referer", g.session.Referer)
	req.Header.Set("User-Agent", g.session.UserAgent)
	req.Header.Set("Accept-Language", g.session.AcceptLanguage)
	req.Header.Set("X-Browser-Channel", g.session.XBrowserChannel)
	req.Header.Set("X-Browser-Copyright", g.session.XBrowserCopyright)
	req.Header.Set("X-Browser-Validation", g.session.XBrowserValidation)
	req.Header.Set("X-Browser-Year", g.session.XBrowserYear)
	req.Header.Set("X-Client-Data", g.session.XClientData)
	req.Header.Set("X-Goog-Api-Key", g.session.APIKey)
	req.Header.Set("X-Goog-AuthUser", g.session.XGoogAuthUser)
	req.Header.Set("X-Goog-Ext-519733851-Bin", g.session.XGoogExt519733851)
	req.Header.Set("X-User-Agent", aiStudioRPCXUserAgent)

	if g.session.Cookie != "" {
		req.Header.Set("Cookie", g.session.Cookie)
	}
	if strings.TrimSpace(authHeader) != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("AI Studio RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func parseAIStudioFinalAnswer(raw []byte) (string, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("parse AI Studio RPC response: %w", err)
	}

	candidates := collectAIStudioModelTexts(payload)
	for i := len(candidates) - 1; i >= 0; i-- {
		clean := strings.TrimSpace(candidates[i])
		if clean == "" {
			continue
		}
		return clean, nil
	}
	return "", fmt.Errorf("AI Studio RPC response did not contain model text")
}

func parseAIStudioAccessToken(raw []byte) (string, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("parse AI Studio access token response: %w", err)
	}

	token := findAIStudioAccessToken(payload)
	if token == "" {
		return "", fmt.Errorf("AI Studio access token response did not contain bearer token")
	}
	return token, nil
}

func findAIStudioAccessToken(node any) string {
	switch typed := node.(type) {
	case string:
		token := strings.TrimSpace(typed)
		if strings.HasPrefix(token, "ya29.") {
			return token
		}
		return ""
	case []any:
		// GenerateAccessToken response is usually ["ya29...."].
		if len(typed) > 0 {
			if token, ok := typed[0].(string); ok {
				token = strings.TrimSpace(token)
				if strings.HasPrefix(token, "ya29.") {
					return token
				}
			}
		}
		for _, child := range typed {
			if token := findAIStudioAccessToken(child); token != "" {
				return token
			}
		}
	}
	return ""
}

func collectAIStudioModelTexts(node any) []string {
	out := make([]string, 0, 16)
	var walk func(any)

	walk = func(current any) {
		arr, ok := current.([]any)
		if !ok {
			return
		}

		if len(arr) >= 2 {
			if role, ok := arr[1].(string); ok && role == "model" {
				out = append(out, collectAIStudioReadableStrings(arr[0])...)
			}
		}

		for _, child := range arr {
			walk(child)
		}
	}

	walk(node)
	return dedupeNonEmptyStrings(out)
}

func collectAIStudioReadableStrings(node any) []string {
	out := make([]string, 0, 16)
	var walk func(any)

	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			if isAIStudioReadableText(typed) {
				out = append(out, typed)
			}
		case []any:
			if len(typed) >= 2 {
				if s, ok := typed[1].(string); ok && isAIStudioReadableText(s) {
					out = append(out, s)
				}
			}
			if len(typed) >= 1 {
				if s, ok := typed[0].(string); ok && isAIStudioReadableText(s) {
					out = append(out, s)
				}
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}

	walk(node)
	return out
}

func isAIStudioReadableText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len(s) > 8192 {
		return false
	}
	if looksLikeOpaqueAIStudioToken(s) {
		return false
	}
	return true
}

func looksLikeOpaqueAIStudioToken(s string) bool {
	if len(s) < 160 {
		return false
	}
	if strings.ContainsAny(s, " \n\t") {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '+' || r == '/' || r == '=' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func dedupeNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
}

func summarizeAIStudioBody(body []byte, maxLen int) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 500
	}
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}

	runes := []rune(text)
	return string(runes[:maxLen]) + "..."
}

func splitAIStudioText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 140
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}
	parts := make([]string, 0, (len(runes)/maxRunes)+1)
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}
