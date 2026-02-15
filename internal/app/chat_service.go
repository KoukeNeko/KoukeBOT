package app

import (
	"sync"
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logChat = logger.New("chat", logger.Cyan)
var tripletParseFailedCount uint64

const personaConfigsBaseDir = "configs/personas"

const (
	DefaultMaxToolRounds            = 3
	DefaultMaxWebSearchCallsPerTurn = 2
)

// ToolExecutionPolicy controls per-request tool-loop behavior and resilience.
type ToolExecutionPolicy struct {
	MaxToolRounds                  int
	MaxWebSearchCallsPerTurn       int
	ForceFinalAnswerAfterToolLimit bool
	DiscordShowInternalToolErrors  bool
}

func DefaultToolExecutionPolicy() ToolExecutionPolicy {
	return ToolExecutionPolicy{
		MaxToolRounds:                  DefaultMaxToolRounds,
		MaxWebSearchCallsPerTurn:       DefaultMaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: true,
		DiscordShowInternalToolErrors:  true,
	}
}

// ChatService orchestrates the full chat pipeline:
// receive message → load conversation → assemble context → call LLM → persist → return.
type ChatService struct {
	provider          llm.Provider
	assembler         *ContextAssembler
	store             store.ConversationRepository
	imageStore        *store.ImageStore // persists images to disk (nil = disabled)
	imageHistoryTurns int               // how many recent user turns to load images for (default 3)
	personas          map[string]*domain.PersonaModule
	toolRegistry      map[string]domain.Tool // tool name → tool instance
	contextManager    *ContextManager        // pre-compaction memory flush (nil = disabled)
	toolLoopRunner    ToolLoopRunner
	memoryFlushRunner MemoryFlushRunner
	multimodalRunner  MultimodalRunner

	toolPolicyMu sync.RWMutex
	toolPolicy   ToolExecutionPolicy

	metricToolGuardDuplicateBlocked uint64
	metricToolGuardLimitBlocked     uint64
	metricToolGuardMaxRounds        uint64
	metricToolForcedFinalSuccess    uint64
	metricToolForcedFinalFailed     uint64
	metricToolLoopFallback          uint64
}

func NewChatService(
	provider llm.Provider,
	assembler *ContextAssembler,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore, // nil = disabled (images won't be persisted)
	imageHistoryTurns int, // 0 → default 3
	personas map[string]*domain.PersonaModule,
	tools []domain.Tool,
	contextManager *ContextManager, // nil = disabled
) *ChatService {
	if imageHistoryTurns <= 0 {
		imageHistoryTurns = 3
	}

	// Build tool registry
	toolRegistry := make(map[string]domain.Tool)
	for _, tool := range tools {
		toolRegistry[tool.Name()] = tool
	}

	service := &ChatService{
		provider:          provider,
		assembler:         assembler,
		store:             convStore,
		imageStore:        imageStore,
		imageHistoryTurns: imageHistoryTurns,
		personas:          personas,
		toolRegistry:      toolRegistry,
		contextManager:    contextManager,
		toolPolicy:        DefaultToolExecutionPolicy(),
	}
	service.toolLoopRunner = defaultToolLoopRunner{service: service}
	service.memoryFlushRunner = defaultMemoryFlushRunner{service: service}
	service.multimodalRunner = defaultMultimodalRunner{service: service}

	return service
}

// UpdatePromptGuardPolicy hot-swaps assembler prompt guard settings without restart.
func (s *ChatService) UpdatePromptGuardPolicy(policy PromptGuardPolicy) {
	if s == nil || s.assembler == nil {
		return
	}
	s.assembler.UpdatePromptGuardPolicy(policy)
}

// UpdateToolExecutionPolicy hot-swaps tool loop policy without restart.
func (s *ChatService) UpdateToolExecutionPolicy(policy ToolExecutionPolicy) {
	if s == nil {
		return
	}
	s.toolPolicyMu.Lock()
	s.toolPolicy = normalizeToolExecutionPolicy(policy)
	s.toolPolicyMu.Unlock()
}

func (s *ChatService) toolExecutionPolicySnapshot() ToolExecutionPolicy {
	if s == nil {
		return DefaultToolExecutionPolicy()
	}
	s.toolPolicyMu.RLock()
	policy := s.toolPolicy
	s.toolPolicyMu.RUnlock()
	return normalizeToolExecutionPolicy(policy)
}

func normalizeToolExecutionPolicy(policy ToolExecutionPolicy) ToolExecutionPolicy {
	defaults := DefaultToolExecutionPolicy()
	if policy.MaxToolRounds < 1 {
		policy.MaxToolRounds = defaults.MaxToolRounds
	}
	if policy.MaxToolRounds > 6 {
		policy.MaxToolRounds = 6
	}
	if policy.MaxWebSearchCallsPerTurn < 1 {
		policy.MaxWebSearchCallsPerTurn = defaults.MaxWebSearchCallsPerTurn
	}
	if policy.MaxWebSearchCallsPerTurn > 4 {
		policy.MaxWebSearchCallsPerTurn = 4
	}
	return policy
}

func (s *ChatService) incrementToolGuardDuplicateBlocked() {
	atomic.AddUint64(&s.metricToolGuardDuplicateBlocked, 1)
}

func (s *ChatService) incrementToolGuardLimitBlocked() {
	atomic.AddUint64(&s.metricToolGuardLimitBlocked, 1)
}

func (s *ChatService) incrementToolGuardMaxRounds() {
	atomic.AddUint64(&s.metricToolGuardMaxRounds, 1)
}

func (s *ChatService) incrementToolForcedFinalSuccess() {
	atomic.AddUint64(&s.metricToolForcedFinalSuccess, 1)
}

func (s *ChatService) incrementToolForcedFinalFailed() {
	atomic.AddUint64(&s.metricToolForcedFinalFailed, 1)
}

func (s *ChatService) incrementToolLoopFallback() {
	atomic.AddUint64(&s.metricToolLoopFallback, 1)
}
