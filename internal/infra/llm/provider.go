package llm

import (
	"context"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// ModelCapabilities describes what a model supports,
// used by the Assembler to choose the correct context topology.
type ModelCapabilities struct {
	SupportsThinking  bool   // Thinking/CoT model (e.g., Gemini 2.5 Pro+)
	ContextWindow     int    // Max tokens in context
	SupportsStreaming bool   // Whether streaming is available
	ProviderName      string // Human-readable provider identifier
}

// ToolMode controls whether providers should expose tool declarations for a request.
type ToolMode string

const (
	ToolModeNormal   ToolMode = "normal"
	ToolModeDisabled ToolMode = "disabled"
)

// ChatRequest is the input to a provider.
type ChatRequest struct {
	SystemPrompt string
	Messages     []domain.Message
	Generation   domain.GenerationConfig
	ToolMode     ToolMode
}

// ChatResponse is a complete (non-streaming) response.
type ChatResponse struct {
	Content      string
	FinishReason string
	TokensUsed   int
}

// StreamChunk is a single piece of a streaming response.
type StreamChunk struct {
	Content    string
	Done       bool
	Error      error
	ToolCalls  []domain.ToolCall      // Function calls requested by LLM
	ToolStatus string                 // Tool execution status: "tool_start", "tool_executing", "tool_error", "tool_done"
	ToolName   string                 // Name of the tool being executed
	ToolInput  map[string]interface{} // Tool input parameters (optional)
	ToolError  string                 // Tool error summary when ToolStatus == "tool_error"
	TokensUsed int                    // Total tokens for this turn (sent in final chunk)
}

// Provider is the strategy interface for LLM backends.
// Each adapter (Gemini API, Vertex AI, Gemini Web, OpenAI-compat)
// implements this interface independently.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
	StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan StreamChunk, error)
	ModelInfo() ModelCapabilities

	// RegisterTools registers tools that the LLM can call during conversation.
	// Not all providers may support function calling.
	RegisterTools(tools []domain.Tool)
}
