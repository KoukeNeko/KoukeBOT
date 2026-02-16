package chat

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
	DefaultMaxToolRounds            = 5
	DefaultMaxWebSearchCallsPerTurn = 2
)

// ToolExecutionPolicy controls per-request tool-loop behavior and resilience.
type ToolExecutionPolicy struct {
	ToolGuardEnabled               bool
	MaxToolRounds                  int
	MaxWebSearchCallsPerTurn       int
	ForceFinalAnswerAfterToolLimit bool
	DiscordShowInternalToolErrors  bool
}

// MemoryRetentionPolicy controls how much recent raw history to preserve.
type MemoryRetentionPolicy struct {
	RecentRounds int // one round = user + assistant
}

func DefaultToolExecutionPolicy() ToolExecutionPolicy {
	return ToolExecutionPolicy{
		ToolGuardEnabled:               false,
		MaxToolRounds:                  DefaultMaxToolRounds,
		MaxWebSearchCallsPerTurn:       DefaultMaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: true,
		DiscordShowInternalToolErrors:  true,
	}
}

func DefaultMemoryRetentionPolicy() MemoryRetentionPolicy {
	return MemoryRetentionPolicy{
		RecentRounds: defaultMemoryRecentRounds,
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
	toolRegistryMu    sync.RWMutex
	contextManager    *ContextManager // pre-compaction memory flush (nil = disabled)
	toolLoopRunner    ToolLoopRunner
	memoryFlushRunner MemoryFlushRunner
	multimodalRunner  MultimodalRunner

	toolPolicyMu   sync.RWMutex
	toolPolicy     ToolExecutionPolicy
	memoryPolicyMu sync.RWMutex
	memoryPolicy   MemoryRetentionPolicy

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
		memoryPolicy:      DefaultMemoryRetentionPolicy(),
	}
	service.applyMemoryRetentionPolicy(service.memoryPolicy)
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

// UpdateMemoryRetentionPolicy hot-swaps recent-history retention without restart.
func (s *ChatService) UpdateMemoryRetentionPolicy(policy MemoryRetentionPolicy) {
	if s == nil {
		return
	}
	normalized := normalizeMemoryRetentionPolicy(policy)
	s.memoryPolicyMu.Lock()
	s.memoryPolicy = normalized
	s.memoryPolicyMu.Unlock()
	s.applyMemoryRetentionPolicy(normalized)
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

func (s *ChatService) memoryRetentionPolicySnapshot() MemoryRetentionPolicy {
	if s == nil {
		return DefaultMemoryRetentionPolicy()
	}
	s.memoryPolicyMu.RLock()
	policy := s.memoryPolicy
	s.memoryPolicyMu.RUnlock()
	return normalizeMemoryRetentionPolicy(policy)
}

func (s *ChatService) keepRecentMessagesSnapshot() int {
	return roundsToRecentMessages(s.memoryRetentionPolicySnapshot().RecentRounds)
}

// UpdateToolRegistry hot-swaps tool declarations and runtime handlers.
func (s *ChatService) UpdateToolRegistry(tools []domain.Tool) {
	if s == nil {
		return
	}
	next := make(map[string]domain.Tool, len(tools))
	for _, tool := range tools {
		next[tool.Name()] = tool
	}
	s.toolRegistryMu.Lock()
	s.toolRegistry = next
	s.toolRegistryMu.Unlock()
}

func (s *ChatService) applyMemoryRetentionPolicy(policy MemoryRetentionPolicy) {
	recentMessages := roundsToRecentMessages(policy.RecentRounds)
	if s.assembler != nil {
		s.assembler.UpdateMemoryRecentMessages(recentMessages)
	}
	if s.contextManager != nil {
		s.contextManager.UpdateKeepRecentMessages(recentMessages)
	}
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

func normalizeMemoryRetentionPolicy(policy MemoryRetentionPolicy) MemoryRetentionPolicy {
	defaults := DefaultMemoryRetentionPolicy()
	if policy.RecentRounds < minMemoryRecentMessages/2 {
		policy.RecentRounds = defaults.RecentRounds
	}
	if policy.RecentRounds > maxMemoryRecentMessages/2 {
		policy.RecentRounds = maxMemoryRecentMessages / 2
	}
	return policy
}

func roundsToRecentMessages(rounds int) int {
	return normalizeMemoryRetentionPolicy(MemoryRetentionPolicy{RecentRounds: rounds}).RecentRounds * 2
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
