package chat

import (
	"context"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// ToolLoopRunner isolates tool-loop state machine execution from chat entrypoints.
type ToolLoopRunner interface {
	Run(ctx context.Context, req ToolLoopRunRequest) <-chan llm.StreamChunk
}

// MemoryFlushRunner isolates memory flush behavior from chat entrypoints.
type MemoryFlushRunner interface {
	Flush(ctx context.Context, req MemoryFlushRequest) error
}

// MultimodalRunner isolates multimodal streaming continuation from chat entrypoints.
type MultimodalRunner interface {
	StreamWithHistory(ctx context.Context, req MultimodalRunRequest) (<-chan llm.StreamChunk, error)
}

// ToolLoopRunRequest is the full state required for one tool-loop stream lifecycle.
type ToolLoopRunRequest struct {
	StreamCh          <-chan llm.StreamChunk
	Conversation      *domain.Conversation
	MessageToStore    string
	ContextForLLM     string
	Persona           *domain.PersonaModule
	PersonaID         string
	RawLLMInput       string
	RawLLMInputHuman  string
	ImageRefs         []domain.ImageRef
	MultimodalHistory []domain.MultimodalMessage
}

// MemoryFlushRequest is the input for a pre-compaction flush pass.
type MemoryFlushRequest struct {
	Conversation *domain.Conversation
	Persona      *domain.PersonaModule
	PersonaID    string
}

// MultimodalRunRequest is the input for multimodal history streaming.
type MultimodalRunRequest struct {
	Conversation     *domain.Conversation
	Persona          *domain.PersonaModule
	PersonaID        string
	ContextForLLM    string
	MessageToStore   string
	CurrentImages    []domain.ImageInput
	HistoryEntries   []domain.ChannelHistoryEntry
	CurrentImageRefs []domain.ImageRef
}

type defaultToolLoopRunner struct{ service *ChatService }

type defaultMemoryFlushRunner struct{ service *ChatService }

type defaultMultimodalRunner struct{ service *ChatService }

func (r defaultToolLoopRunner) Run(ctx context.Context, req ToolLoopRunRequest) <-chan llm.StreamChunk {
	return r.service.handleToolCallingStream(
		ctx,
		req.StreamCh,
		req.Conversation,
		req.MessageToStore,
		req.ContextForLLM,
		req.Persona,
		req.PersonaID,
		req.RawLLMInput,
		req.RawLLMInputHuman,
		req.ImageRefs,
		req.MultimodalHistory,
	)
}

func (r defaultMemoryFlushRunner) Flush(ctx context.Context, req MemoryFlushRequest) error {
	return r.service.executeMemoryFlush(ctx, req.Conversation, req.Persona, req.PersonaID)
}

func (r defaultMultimodalRunner) StreamWithHistory(ctx context.Context, req MultimodalRunRequest) (<-chan llm.StreamChunk, error) {
	return r.service.streamWithMultimodalHistory(
		ctx,
		req.Conversation,
		req.Persona,
		req.PersonaID,
		req.ContextForLLM,
		req.MessageToStore,
		req.CurrentImages,
		req.HistoryEntries,
		req.CurrentImageRefs...,
	)
}
