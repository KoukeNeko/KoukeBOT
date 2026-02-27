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
	tools  []domain.Tool // Registered tools for function calling
}

// NewGeminiAPIProvider creates a provider using a Google AI Studio API key.
func NewGeminiAPIProvider(ctx context.Context, apiKey, model string) (*GeminiAPIProvider, error) {
	var cfg *genai.ClientConfig
	if key := strings.TrimSpace(apiKey); key != "" {
		// Keep explicit key support while staying aligned with official SDK usage.
		cfg = &genai.ClientConfig{APIKey: key}
	}
	client, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	return &GeminiAPIProvider{
		client: client,
		model:  NormalizeModelID(model),
	}, nil
}

func (g *GeminiAPIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	contents := buildContents(req.Messages)
	config := buildConfig(req, g.model)

	// Prepare config with optional tools and provider-compatible generation fields.
	genConfig := buildGenerateContentConfig(req.SystemPrompt, config)

	// Add tools if registered and request allows function calling.
	if req.ToolMode != ToolModeDisabled && len(g.tools) > 0 {
		genConfig.Tools = convertToGeminiTools(g.tools)
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, genConfig)
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
	config := buildConfig(req, g.model)
	ch := make(chan StreamChunk, 16)

	// Prepare config with optional tools and provider-compatible generation fields.
	genConfig := buildGenerateContentConfig(req.SystemPrompt, config)

	// Add tools if registered and request allows function calling.
	if req.ToolMode != ToolModeDisabled && len(g.tools) > 0 {
		genConfig.Tools = convertToGeminiTools(g.tools)
	}

	go func() {
		defer close(ch)

		var lastTokenCount int
		var lastFinishReason string
		iter := g.client.Models.GenerateContentStream(ctx, g.model, contents, genConfig)

		for resp, err := range iter {
			if err != nil {
				ch <- StreamChunk{Error: err, Done: true}
				return
			}

			if resp.UsageMetadata != nil {
				lastTokenCount = int(resp.UsageMetadata.TotalTokenCount)
			}
			if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
				lastFinishReason = string(resp.Candidates[0].FinishReason)
			}

			// Check for function calls
			toolCalls := extractFunctionCalls(resp)
			if len(toolCalls) > 0 {
				ch <- StreamChunk{ToolCalls: toolCalls, Done: true, TokensUsed: lastTokenCount}
				return
			}

			// Regular text content
			text := extractText(resp)
			if text != "" {
				ch <- StreamChunk{Content: text}
			}
		}
		ch <- StreamChunk{Done: true, TokensUsed: lastTokenCount, FinishReason: lastFinishReason}
	}()

	return ch, nil
}

func (g *GeminiAPIProvider) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan StreamChunk, error) {
	contents := buildMultimodalContents(messages)
	config := buildGenerationConfig(generation, g.model)
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)

		var lastTokenCount int
		genConfig := buildGenerateContentConfig(systemPrompt, config)
		iter := g.client.Models.GenerateContentStream(ctx, g.model, contents, genConfig)

		for resp, err := range iter {
			if err != nil {
				ch <- StreamChunk{Error: err, Done: true}
				return
			}
			if resp.UsageMetadata != nil {
				lastTokenCount = int(resp.UsageMetadata.TotalTokenCount)
			}
			text := extractText(resp)
			if text != "" {
				ch <- StreamChunk{Content: text}
			}
		}
		ch <- StreamChunk{Done: true, TokensUsed: lastTokenCount}
	}()

	return ch, nil
}

func (g *GeminiAPIProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     1_000_000,
		MaxOutputTokens:   resolveMaxTokensForModel(g.model, defaultMaxOutputTokens),
		SupportsStreaming: true,
		ProviderName:      "gemini-api",
		Model:             g.model,
	}
}

type genConfig struct {
	temperature      float64
	maxTokens        int
	topP             float64
	frequencyPenalty float64
	presencePenalty  float64
}

func buildGenerateContentConfig(systemPrompt string, cfg genConfig) *genai.GenerateContentConfig {
	genConfig := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
		Temperature:       genai.Ptr(float32(cfg.temperature)),
		MaxOutputTokens:   int32(cfg.maxTokens),
		TopP:              genai.Ptr(float32(cfg.topP)),
	}
	// Some Gemini models reject penalty fields unless the feature is enabled.
	if cfg.frequencyPenalty != 0 {
		genConfig.FrequencyPenalty = genai.Ptr(float32(cfg.frequencyPenalty))
	}
	if cfg.presencePenalty != 0 {
		genConfig.PresencePenalty = genai.Ptr(float32(cfg.presencePenalty))
	}
	return genConfig
}

// defaultMaxOutputTokens is the baseline maxOutputTokens for non-thinking models.
const defaultMaxOutputTokens = 8192

// minThinkingModelOutputTokens is the minimum maxOutputTokens for thinking models
// (Gemini 2.5+, 3.x). Thinking models consume maxOutputTokens for BOTH internal
// reasoning tokens AND visible output. With a low limit (e.g. 4096), the model may
// exhaust the budget on thinking, leaving insufficient tokens for visible output.
const minThinkingModelOutputTokens = 16384

func buildConfig(req ChatRequest, model string) genConfig {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        defaultMaxOutputTokens,
		topP:             0.95,
		frequencyPenalty: 0.0,
		presencePenalty:  0.0,
	}
	if req.Generation.Temperature > 0 {
		cfg.temperature = req.Generation.Temperature
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
	cfg.maxTokens = resolveMaxTokensForModel(model, cfg.maxTokens)
	return cfg
}

// resolveMaxTokensForModel ensures thinking models have a sufficient maxOutputTokens
// floor. Thinking models (Gemini 2.5+, 3.x) share the maxOutputTokens budget between
// internal reasoning and visible output; a low limit truncates the visible response.
func resolveMaxTokensForModel(model string, configuredMaxTokens int) int {
	isThinkingModel := strings.Contains(model, "2.5") || strings.Contains(model, "3")
	if !isThinkingModel {
		return configuredMaxTokens
	}
	if configuredMaxTokens < minThinkingModelOutputTokens {
		return minThinkingModelOutputTokens
	}
	return configuredMaxTokens
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
func buildGenerationConfig(gen domain.GenerationConfig, model string) genConfig {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        defaultMaxOutputTokens,
		topP:             0.95,
		frequencyPenalty: 0.0,
		presencePenalty:  0.0,
	}
	if gen.Temperature > 0 {
		cfg.temperature = gen.Temperature
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
	cfg.maxTokens = resolveMaxTokensForModel(model, cfg.maxTokens)
	return cfg
}

// RegisterTools registers tools that the LLM can call during conversation
func (g *GeminiAPIProvider) RegisterTools(tools []domain.Tool) {
	g.tools = tools
}

// convertToGeminiTools converts domain.Tool to genai.Tool format for Gemini API
func convertToGeminiTools(tools []domain.Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	declarations := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		schema := convertJSONSchemaToGeminiSchema(tool.InputSchema())
		decl := &genai.FunctionDeclaration{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  schema,
		}
		declarations = append(declarations, decl)
	}

	return []*genai.Tool{{FunctionDeclarations: declarations}}
}

// convertJSONSchemaToGeminiSchema converts a JSON Schema map to genai.Schema.
func convertJSONSchemaToGeminiSchema(schema map[string]interface{}) *genai.Schema {
	converted := convertPropertyToSchema(schema)
	if converted.Type == "" {
		converted.Type = genai.TypeObject
	}
	return converted
}

// convertPropertyToSchema converts a property definition to genai.Schema recursively.
func convertPropertyToSchema(prop map[string]interface{}) *genai.Schema {
	schema := &genai.Schema{}

	if schemaType := jsonSchemaTypeToGeminiType(prop); schemaType != "" {
		schema.Type = schemaType
	}
	if schema.Type == "" {
		if _, hasProps := prop["properties"]; hasProps {
			schema.Type = genai.TypeObject
		} else if _, hasItems := prop["items"]; hasItems {
			schema.Type = genai.TypeArray
		}
	}

	if desc, ok := prop["description"].(string); ok {
		schema.Description = desc
	}
	if title, ok := prop["title"].(string); ok {
		schema.Title = title
	}
	if format, ok := prop["format"].(string); ok {
		schema.Format = format
	}
	if nullable, ok := prop["nullable"].(bool); ok {
		schema.Nullable = &nullable
	}

	if min, ok := asFloat64(prop["minimum"]); ok {
		schema.Minimum = &min
	}
	if max, ok := asFloat64(prop["maximum"]); ok {
		schema.Maximum = &max
	}
	if minLen, ok := asInt64(prop["minLength"]); ok {
		schema.MinLength = &minLen
	}
	if maxLen, ok := asInt64(prop["maxLength"]); ok {
		schema.MaxLength = &maxLen
	}
	if minItems, ok := asInt64(prop["minItems"]); ok {
		schema.MinItems = &minItems
	}
	if maxItems, ok := asInt64(prop["maxItems"]); ok {
		schema.MaxItems = &maxItems
	}

	if required := toStringSlice(prop["required"]); len(required) > 0 {
		schema.Required = required
	}
	if enumVals := toStringSlice(prop["enum"]); len(enumVals) > 0 {
		schema.Enum = enumVals
	}
	if anyOfRaw, ok := prop["anyOf"].([]interface{}); ok {
		anyOf := make([]*genai.Schema, 0, len(anyOfRaw))
		for _, entry := range anyOfRaw {
			if entryMap, ok := entry.(map[string]interface{}); ok {
				anyOf = append(anyOf, convertPropertyToSchema(entryMap))
			}
		}
		if len(anyOf) > 0 {
			schema.AnyOf = anyOf
		}
	}

	if propsRaw, ok := prop["properties"].(map[string]interface{}); ok {
		properties := make(map[string]*genai.Schema, len(propsRaw))
		for key, val := range propsRaw {
			if propMap, ok := val.(map[string]interface{}); ok {
				properties[key] = convertPropertyToSchema(propMap)
			}
		}
		if len(properties) > 0 {
			schema.Properties = properties
		}
	}

	if schema.Type == genai.TypeArray {
		if itemsRaw, ok := prop["items"].(map[string]interface{}); ok && len(itemsRaw) > 0 {
			schema.Items = convertPropertyToSchema(itemsRaw)
		} else {
			// Cloud Code Assist requires items for array schema definitions.
			schema.Items = &genai.Schema{Type: genai.TypeString}
		}
	}

	return schema
}

func jsonSchemaTypeToGeminiType(prop map[string]interface{}) genai.Type {
	switch typeVal := prop["type"].(type) {
	case string:
		switch typeVal {
		case "string":
			return genai.TypeString
		case "integer":
			return genai.TypeInteger
		case "number":
			return genai.TypeNumber
		case "boolean":
			return genai.TypeBoolean
		case "array":
			return genai.TypeArray
		case "object":
			return genai.TypeObject
		}
	case []interface{}:
		// Handle ["string", "null"] patterns by selecting the first non-null type.
		for _, entry := range typeVal {
			if s, ok := entry.(string); ok && s != "null" {
				return jsonSchemaTypeToGeminiType(map[string]interface{}{"type": s})
			}
		}
	}
	return ""
}

func toStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func asFloat64(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func asInt64(raw interface{}) (int64, bool) {
	switch v := raw.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	default:
		return 0, false
	}
}

// extractFunctionCalls extracts function calls from Gemini response
func extractFunctionCalls(resp *genai.GenerateContentResponse) []domain.ToolCall {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return nil
	}

	var toolCalls []domain.ToolCall
	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:    part.FunctionCall.Name, // Use function name as ID for simplicity
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		}
	}

	return toolCalls
}
