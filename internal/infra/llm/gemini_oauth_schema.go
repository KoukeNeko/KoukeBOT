package llm

// gemini_oauth_schema.go — Cloud Code Assist API wire types, request builders,
// and Gemini-specific JSON Schema cleaning.
//
// Separation rationale: CA API types and schema transformation change together
// when the API evolves. Keeping them here (separate from provider and transport
// logic) means schema cleaning rules can be updated without touching streaming
// or retry code.

import (
	"encoding/base64"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

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

// --- Request builders ---

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

	// Count images and total size for logging
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
