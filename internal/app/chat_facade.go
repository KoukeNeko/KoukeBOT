package app

import (
	chatpkg "github.com/KoukeNeko/KoukeBOT/internal/app/chat"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// Chat facade types keep existing app-level API stable while chat internals
// live in a dedicated package for clearer project structure.
type (
	ChatService           = chatpkg.ChatService
	ToolExecutionPolicy   = chatpkg.ToolExecutionPolicy
	MemoryRetentionPolicy = chatpkg.MemoryRetentionPolicy
	ToolLoopMetrics       = chatpkg.ToolLoopMetrics
	ContextManager        = chatpkg.ContextManager
	ContextAssembler      = chatpkg.ContextAssembler
	PromptGuardPolicy     = chatpkg.PromptGuardPolicy
	AssembledContext      = chatpkg.AssembledContext
)

const (
	AnalysisVisibilityVisible = chatpkg.AnalysisVisibilityVisible
	AnalysisVisibilityHidden  = chatpkg.AnalysisVisibilityHidden
	AnalysisVisibilityDebug   = chatpkg.AnalysisVisibilityDebug
)

func DefaultPromptGuardPolicy() PromptGuardPolicy {
	return chatpkg.DefaultPromptGuardPolicy()
}

func DefaultToolExecutionPolicy() ToolExecutionPolicy {
	return chatpkg.DefaultToolExecutionPolicy()
}

func DefaultMemoryRetentionPolicy() MemoryRetentionPolicy {
	return chatpkg.DefaultMemoryRetentionPolicy()
}

func NewContextManager(flushThreshold, windowSize int) *ContextManager {
	return chatpkg.NewContextManager(flushThreshold, windowSize)
}

func EstimateConversationContextTokens(conv *domain.Conversation) int {
	return chatpkg.EstimateConversationContextTokens(conv)
}

func NewContextAssembler() *ContextAssembler {
	return chatpkg.NewContextAssembler()
}

func NewChatService(
	provider llm.Provider,
	assembler *ContextAssembler,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	imageHistoryTurns int,
	personas map[string]*domain.PersonaModule,
	tools []domain.Tool,
	contextManager *ContextManager,
) *ChatService {
	return chatpkg.NewChatService(
		provider,
		assembler,
		convStore,
		imageStore,
		imageHistoryTurns,
		personas,
		tools,
		contextManager,
	)
}
