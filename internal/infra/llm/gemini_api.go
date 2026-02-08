package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"google.golang.org/genai"
)

// GeminiAPIProvider uses Google AI Studio API key auth.
type GeminiAPIProvider struct {
	client *genai.Client
	model  string
}

// NewGeminiAPIProvider creates a provider using a Google AI Studio API key.
func NewGeminiAPIProvider(ctx context.Context, apiKey, model string) (*GeminiAPIProvider, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	return &GeminiAPIProvider{
		client: client,
		model:  model,
	}, nil
}

func (g *GeminiAPIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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
		return nil, fmt.Errorf("gemini generate: %w", err)
	}

	text := extractText(resp)
	return &ChatResponse{
		Content:    text,
		TokensUsed: int(resp.UsageMetadata.TotalTokenCount),
	}, nil
}

func (g *GeminiAPIProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
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

func (g *GeminiAPIProvider) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan StreamChunk, error) {
	contents := buildMultimodalContents(messages)
	config := buildGenerationConfig(generation)
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)

		iter := g.client.Models.GenerateContentStream(ctx, g.model, contents, &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
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

func (g *GeminiAPIProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3")

	return ModelCapabilities{
		SupportsThinking: supportsThinking,
		ContextWindow:    1_000_000,
		SupportsStreaming: true,
		ProviderName:     "gemini-api",
	}
}

type genConfig struct {
	temperature      float64
	maxTokens        int
	topP             float64
	frequencyPenalty float64
	presencePenalty  float64
}

func buildConfig(req ChatRequest) genConfig {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        8192,
		topP:             0.95,
		frequencyPenalty: 0.0,
		presencePenalty:  0.0,
	}
	if req.Generation.Temperature > 0 {
		cfg.temperature = req.Generation.Temperature
	}
	if req.Generation.MaxTokens > 0 {
		cfg.maxTokens = req.Generation.MaxTokens
	}
	if req.Generation.TopP > 0 {
		cfg.topP = req.Generation.TopP
	}
	if req.Generation.FrequencyPenalty > 0 {
		cfg.frequencyPenalty = req.Generation.FrequencyPenalty
	}
	if req.Generation.PresencePenalty > 0 {
		cfg.presencePenalty = req.Generation.PresencePenalty
	}
	return cfg
}

func buildContents(messages []domain.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{genai.NewPartFromText(msg.Content)},
		})
	}
	return contents
}

func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

// buildMultimodalContents converts multimodal messages to genai Content format.
func buildMultimodalContents(messages []domain.MultimodalMessage) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == domain.RoleAssistant {
			role = "model"
		}

		parts := make([]*genai.Part, 0, len(msg.Parts))
		for _, p := range msg.Parts {
			if p.Text != "" {
				parts = append(parts, genai.NewPartFromText(p.Text))
			}
			if p.ImageData != nil {
				// Use Blob for inline data (images)
				parts = append(parts, &genai.Part{
					InlineData: &genai.Blob{
						MIMEType: p.ImageData.MimeType,
						Data:     p.ImageData.Data,
					},
				})
			}
		}

		if len(parts) > 0 {
			contents = append(contents, &genai.Content{
				Role:  role,
				Parts: parts,
			})
		}
	}
	return contents
}

// buildGenerationConfig creates a genConfig from domain GenerationConfig.
func buildGenerationConfig(gen domain.GenerationConfig) genConfig {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        8192,
		topP:             0.95,
		frequencyPenalty: 0.0,
		presencePenalty:  0.0,
	}
	if gen.Temperature > 0 {
		cfg.temperature = gen.Temperature
	}
	if gen.MaxTokens > 0 {
		cfg.maxTokens = gen.MaxTokens
	}
	if gen.TopP > 0 {
		cfg.topP = gen.TopP
	}
	if gen.FrequencyPenalty > 0 {
		cfg.frequencyPenalty = gen.FrequencyPenalty
	}
	if gen.PresencePenalty > 0 {
		cfg.presencePenalty = gen.PresencePenalty
	}
	return cfg
}
