package llm

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/genai"
)

// GeminiOAuthProvider uses OAuth token + Gemini API backend to access Gemini.
// The Code Assist project doesn't have Vertex AI enabled, so we use the
// Gemini API endpoint (generativelanguage.googleapis.com) with OAuth Bearer auth.
type GeminiOAuthProvider struct {
	client *genai.Client
	model  string
}

// NewGeminiOAuthProvider creates a provider using OAuth credentials via Gemini API backend.
func NewGeminiOAuthProvider(ctx context.Context, tokenData *OAuthTokenData, model string) (*GeminiOAuthProvider, error) {
	tokenSource := NewTokenSource(ctx, tokenData)
	httpClient := oauth2.NewClient(ctx, tokenSource)

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini oauth client: %w", err)
	}

	return &GeminiOAuthProvider{
		client: client,
		model:  model,
	}, nil
}

func (g *GeminiOAuthProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	contents := buildContents(req.Messages)
	config := buildConfig(req)

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(req.SystemPrompt, genai.RoleUser),
		Temperature:       genai.Ptr(float32(config.temperature)),
		MaxOutputTokens:   int32(config.maxTokens),
		TopP:              genai.Ptr(float32(config.topP)),
		FrequencyPenalty:  genai.Ptr(float32(config.frequencyPenalty)),
		PresencePenalty:   genai.Ptr(float32(config.presencePenalty)),
	})
	if err != nil {
		return nil, fmt.Errorf("gemini oauth generate: %w", err)
	}

	text := extractText(resp)
	return &ChatResponse{
		Content:    text,
		TokensUsed: int(resp.UsageMetadata.TotalTokenCount),
	}, nil
}

func (g *GeminiOAuthProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	contents := buildContents(req.Messages)
	config := buildConfig(req)
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)

		iter := g.client.Models.GenerateContentStream(ctx, g.model, contents, &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText(req.SystemPrompt, genai.RoleUser),
			Temperature:       genai.Ptr(float32(config.temperature)),
			MaxOutputTokens:   int32(config.maxTokens),
			TopP:              genai.Ptr(float32(config.topP)),
			FrequencyPenalty:  genai.Ptr(float32(config.frequencyPenalty)),
			PresencePenalty:   genai.Ptr(float32(config.presencePenalty)),
		})

		for resp, err := range iter {
			if err != nil {
				ch <- StreamChunk{Error: err, Done: true}
				return
			}
			text := extractText(resp)
			if text != "" {
				ch <- StreamChunk{Content: text}
			}
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
