package llm

import (
	"context"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// ModelCapabilities describes what a model supports,
// used by the Assembler to choose the correct context topology.
type ModelCapabilities struct {
	SupportsThinking bool   // Thinking/CoT model (e.g., Gemini 2.5 Pro+)
	ContextWindow    int    // Max tokens in context
	SupportsStreaming bool   // Whether streaming is available
	ProviderName     string // Human-readable provider identifier
}

// ChatRequest is the input to a provider.
type ChatRequest struct {
	SystemPrompt string
	Messages     []domain.Message
	Generation   domain.GenerationConfig
}

// ChatResponse is a complete (non-streaming) response.
type ChatResponse struct {
	Content      string
	FinishReason string
	TokensUsed   int
}

// StreamChunk is a single piece of a streaming response.
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// Provider is the strategy interface for LLM backends.
// Each adapter (Gemini API, Vertex AI, Gemini Web, OpenAI-compat)
// implements this interface independently.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
	ModelInfo() ModelCapabilities
}
