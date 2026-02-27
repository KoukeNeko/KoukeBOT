package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const (
	defaultOpenAIMaxCompletionTokens = 8192
	defaultOpenAIModel               = "gpt-5"
)

// OpenAIAPIProvider implements Provider using the official openai-go SDK.
type OpenAIAPIProvider struct {
	client openai.Client
	model  string
	tools  []domain.Tool
}

// NewOpenAIAPIProvider creates an OpenAI provider.
// When apiKey is empty, the SDK follows official behavior and reads OPENAI_API_KEY.
func NewOpenAIAPIProvider(apiKey, model string) (*OpenAIAPIProvider, error) {
	opts := make([]option.RequestOption, 0, 1)
	if key := strings.TrimSpace(apiKey); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	client := openai.NewClient(opts...)

	normalizedModel := NormalizeModelID(model)
	if normalizedModel == "" {
		normalizedModel = defaultOpenAIModel
	}

	return &OpenAIAPIProvider{
		client: client,
		model:  normalizedModel,
	}, nil
}

func (o *OpenAIAPIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	params := o.buildParams(req, buildOpenAIMessages(req.SystemPrompt, req.Messages), true, false)

	resp, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai generate: %w", err)
	}

	if len(resp.Choices) == 0 {
		return &ChatResponse{
			Content:    "",
			TokensUsed: int(resp.Usage.TotalTokens),
		}, nil
	}

	choice := resp.Choices[0]
	content := choice.Message.Content
	if content == "" && choice.Message.Refusal != "" {
		content = choice.Message.Refusal
	}

	return &ChatResponse{
		Content:      content,
		FinishReason: choice.FinishReason,
		TokensUsed:   int(resp.Usage.TotalTokens),
	}, nil
}

func (o *OpenAIAPIProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	params := o.buildParams(req, buildOpenAIMessages(req.SystemPrompt, req.Messages), true, true)
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)
		defer stream.Close()

		var acc openai.ChatCompletionAccumulator
		for stream.Next() {
			chunk := stream.Current()
			if !acc.AddChunk(chunk) {
				ch <- StreamChunk{Error: fmt.Errorf("openai stream accumulation mismatch"), Done: true}
				return
			}

			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- StreamChunk{Content: choice.Delta.Content}
				}
				if choice.Delta.Refusal != "" {
					ch <- StreamChunk{Content: choice.Delta.Refusal}
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("openai stream: %w", err), Done: true}
			return
		}

		var (
			finishReason string
			toolCalls    []domain.ToolCall
		)
		if len(acc.Choices) > 0 {
			finishReason = acc.Choices[0].FinishReason
			toolCalls = extractOpenAIToolCalls(acc.Choices[0].Message.ToolCalls)
		}
		if len(toolCalls) > 0 {
			ch <- StreamChunk{
				ToolCalls:    toolCalls,
				Done:         true,
				TokensUsed:   int(acc.Usage.TotalTokens),
				FinishReason: finishReason,
			}
			return
		}
		ch <- StreamChunk{
			Done:         true,
			TokensUsed:   int(acc.Usage.TotalTokens),
			FinishReason: finishReason,
		}
	}()

	return ch, nil
}

func (o *OpenAIAPIProvider) StreamChatMultimodal(
	ctx context.Context,
	systemPrompt string,
	messages []domain.MultimodalMessage,
	generation domain.GenerationConfig,
) (<-chan StreamChunk, error) {
	req := ChatRequest{
		SystemPrompt: systemPrompt,
		Generation:   generation,
		ToolMode:     ToolModeDisabled,
	}
	params := o.buildParams(req, buildOpenAIMultimodalMessages(systemPrompt, messages), false, true)
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)
		defer stream.Close()

		var acc openai.ChatCompletionAccumulator
		for stream.Next() {
			chunk := stream.Current()
			if !acc.AddChunk(chunk) {
				ch <- StreamChunk{Error: fmt.Errorf("openai multimodal stream accumulation mismatch"), Done: true}
				return
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- StreamChunk{Content: choice.Delta.Content}
				}
				if choice.Delta.Refusal != "" {
					ch <- StreamChunk{Content: choice.Delta.Refusal}
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("openai multimodal stream: %w", err), Done: true}
			return
		}

		finishReason := ""
		if len(acc.Choices) > 0 {
			finishReason = acc.Choices[0].FinishReason
		}
		ch <- StreamChunk{
			Done:         true,
			TokensUsed:   int(acc.Usage.TotalTokens),
			FinishReason: finishReason,
		}
	}()

	return ch, nil
}

func (o *OpenAIAPIProvider) ModelInfo() ModelCapabilities {
	lowerModel := strings.ToLower(o.model)
	supportsThinking := strings.Contains(lowerModel, "gpt-5") || strings.HasPrefix(lowerModel, "o")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     400_000,
		MaxOutputTokens:   defaultOpenAIMaxCompletionTokens,
		SupportsStreaming: true,
		ProviderName:      "openai-api",
		Model:             o.model,
	}
}

func (o *OpenAIAPIProvider) RegisterTools(tools []domain.Tool) {
	o.tools = tools
}

func (o *OpenAIAPIProvider) buildParams(
	req ChatRequest,
	messages []openai.ChatCompletionMessageParamUnion,
	allowTools bool,
	includeUsage bool,
) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Messages:            messages,
		Model:               shared.ChatModel(o.model),
		MaxCompletionTokens: openai.Int(defaultOpenAIMaxCompletionTokens),
	}

	if req.Generation.Temperature > 0 {
		params.Temperature = openai.Float(req.Generation.Temperature)
	}
	if req.Generation.TopP > 0 {
		params.TopP = openai.Float(req.Generation.TopP)
	}
	if req.Generation.FrequencyPenalty != 0 {
		params.FrequencyPenalty = openai.Float(req.Generation.FrequencyPenalty)
	}
	if req.Generation.PresencePenalty != 0 {
		params.PresencePenalty = openai.Float(req.Generation.PresencePenalty)
	}
	if includeUsage {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}
	if allowTools && req.ToolMode != ToolModeDisabled && len(o.tools) > 0 {
		params.Tools = convertToOpenAITools(o.tools)
	}
	return params
}

func buildOpenAIMessages(systemPrompt string, messages []domain.Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		result = append(result, openai.SystemMessage(systemPrompt))
	}
	for _, msg := range messages {
		switch msg.Role {
		case domain.RoleAssistant:
			result = append(result, openai.AssistantMessage(msg.Content))
		case domain.RoleSystem:
			result = append(result, openai.SystemMessage(msg.Content))
		default:
			result = append(result, openai.UserMessage(msg.Content))
		}
	}
	return result
}

func buildOpenAIMultimodalMessages(systemPrompt string, messages []domain.MultimodalMessage) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		result = append(result, openai.SystemMessage(systemPrompt))
	}

	for _, msg := range messages {
		switch msg.Role {
		case domain.RoleAssistant:
			text := joinOpenAITextParts(msg.Parts)
			if text != "" {
				result = append(result, openai.AssistantMessage(text))
			}
		case domain.RoleSystem:
			text := joinOpenAITextParts(msg.Parts)
			if text != "" {
				result = append(result, openai.SystemMessage(text))
			}
		default:
			parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(msg.Parts))
			for _, part := range msg.Parts {
				if strings.TrimSpace(part.Text) != "" {
					parts = append(parts, openai.TextContentPart(part.Text))
				}
				if part.ImageData != nil && len(part.ImageData.Data) > 0 {
					parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
						URL: imageDataURL(part.ImageData.MimeType, part.ImageData.Data),
					}))
				}
			}
			if len(parts) > 0 {
				result = append(result, openai.UserMessage(parts))
			}
		}
	}

	return result
}

func joinOpenAITextParts(parts []domain.MessagePart) string {
	if len(parts) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(part.Text)
	}
	return sb.String()
}

func imageDataURL(mimeType string, data []byte) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func convertToOpenAITools(tools []domain.Tool) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema := map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
		if inputSchema := tool.InputSchema(); inputSchema != nil {
			schema = inputSchema
		}

		result = append(result, openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: shared.FunctionDefinitionParam{
					Name:        tool.Name(),
					Description: openai.String(tool.Description()),
					Parameters:  shared.FunctionParameters(schema),
				},
			},
		})
	}
	return result
}

func extractOpenAIToolCalls(rawCalls []openai.ChatCompletionMessageToolCallUnion) []domain.ToolCall {
	if len(rawCalls) == 0 {
		return nil
	}

	calls := make([]domain.ToolCall, 0, len(rawCalls))
	for _, rawCall := range rawCalls {
		variant := rawCall.AsAny()
		call, ok := variant.(openai.ChatCompletionMessageFunctionToolCall)
		if !ok {
			continue
		}
		input := parseOpenAIToolArguments(call.Function.Arguments)
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = call.Function.Name
		}
		calls = append(calls, domain.ToolCall{
			ID:    callID,
			Name:  call.Function.Name,
			Input: input,
		})
	}

	if len(calls) == 0 {
		return nil
	}
	return calls
}

func parseOpenAIToolArguments(arguments string) map[string]interface{} {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]interface{}{}
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return map[string]interface{}{"_raw": trimmed}
	}
	if parsed == nil {
		return map[string]interface{}{}
	}
	return parsed
}
