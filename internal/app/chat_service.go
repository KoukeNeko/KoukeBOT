package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

	return &ChatService{
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

type ToolLoopMetrics struct {
	ToolGuardDuplicateBlocked uint64 `json:"tool_guard_duplicate_blocked"`
	ToolGuardLimitBlocked     uint64 `json:"tool_guard_limit_blocked"`
	ToolGuardMaxRounds        uint64 `json:"tool_guard_max_rounds"`
	ToolForcedFinalSuccess    uint64 `json:"tool_forced_final_success"`
	ToolForcedFinalFailed     uint64 `json:"tool_forced_final_failed"`
	ToolLoopFallback          uint64 `json:"tool_loop_fallback"`
}

func (s *ChatService) ToolLoopMetricsSnapshot() ToolLoopMetrics {
	if s == nil {
		return ToolLoopMetrics{}
	}
	return ToolLoopMetrics{
		ToolGuardDuplicateBlocked: atomic.LoadUint64(&s.metricToolGuardDuplicateBlocked),
		ToolGuardLimitBlocked:     atomic.LoadUint64(&s.metricToolGuardLimitBlocked),
		ToolGuardMaxRounds:        atomic.LoadUint64(&s.metricToolGuardMaxRounds),
		ToolForcedFinalSuccess:    atomic.LoadUint64(&s.metricToolForcedFinalSuccess),
		ToolForcedFinalFailed:     atomic.LoadUint64(&s.metricToolForcedFinalFailed),
		ToolLoopFallback:          atomic.LoadUint64(&s.metricToolLoopFallback),
	}
}

// HandleMessage processes a user message and returns the assistant response.
func (s *ChatService) HandleMessage(ctx context.Context, channelID, personaID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, channelID, personaID, "", userMessage)
}

// HandleMessageByConversationID processes a message targeting a specific conversation session.
func (s *ChatService) HandleMessageByConversationID(ctx context.Context, conversationID, userMessage string) (string, error) {
	return s.handleMessageInternal(ctx, "", "", conversationID, userMessage)
}

func (s *ChatService) handleMessageInternal(ctx context.Context, channelID, personaID, conversationID, userMessage string) (string, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return "", err
	}
	conv.CumulativeTokens = EstimateConversationContextTokens(conv)

	conv.AppendUserMessage(userMessage)

	modelInfo := s.provider.ModelInfo()
	fmt.Printf("[ChatService] Assembling context - Provider: %s, SupportsThinking: %v\n",
		modelInfo.ProviderName, modelInfo.SupportsThinking)

	assembled, err := s.assembler.Assemble(persona, conv, modelInfo)
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("chat", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)

	fmt.Printf("[ChatService] Calling LLM - System prompt length: %d, Messages: %d\n",
		len(assembled.SystemPrompt), len(assembled.Messages))

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	fmt.Printf("[ChatService] LLM response received - Length: %d chars\n", len(resp.Content))

	rawLLMInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)
	view := s.prepareAssistantOutputForStorage(resp.Content)
	conv.AppendAssistantMessageWithDisplayAndRaw(view.Content, view.Display, rawLLMInput, view.RawOutput)
	s.applyTokenAccounting(conv, resp.TokensUsed)

	if err := s.store.Save(ctx, conv); err != nil {
		return "", fmt.Errorf("save conversation [%s/%s]: %w", conv.ChannelID, resolvedPersonaID, err)
	}

	if view.Display != "" {
		return view.Display, nil
	}
	return view.Content, nil
}

// HandleMessageStream processes a user message and returns a streaming channel.
func (s *ChatService) HandleMessageStream(ctx context.Context, channelID, personaID, userMessage string) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithContext(ctx, channelID, personaID, userMessage, userMessage)
}

// HandleMessageStreamByConversationID processes a user message for a specific conversation session.
func (s *ChatService) HandleMessageStreamByConversationID(ctx context.Context, conversationID, userMessage string) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithContextInternal(ctx, "", "", conversationID, userMessage, userMessage)
}

// HandleMessageStreamWithContext processes a user message with separate display and storage versions.
// contextForLLM: the full message (with channel history) sent to LLM
// messageToStore: the clean message (without channel history markers) saved to conversation store
func (s *ChatService) HandleMessageStreamWithContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithContextInternal(ctx, channelID, personaID, "", contextForLLM, messageToStore)
}

func (s *ChatService) handleMessageStreamWithContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokens(conv)

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.executeMemoryFlush(ctx, conv, persona, resolvedPersonaID); err != nil {
			logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
		}
	}

	// Temporarily append the LLM context version for assembly
	conv.AppendUserMessage(contextForLLM)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("stream", conv.ChannelID, resolvedPersonaID, conv.ID, assembled)

	// Remove the temporary message - we'll store the clean version later
	conv.Messages = conv.Messages[:len(conv.Messages)-1]

	// Build raw LLM input for transparency (before streaming)
	rawLLMInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	// Handle tool calling loop and wrap stream for persistence
	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, resolvedPersonaID, rawLLMInput, nil, nil), nil
}

// HandleMessageStreamWithImages processes a user message with images and returns a streaming channel.
func (s *ChatService) HandleMessageStreamWithImages(ctx context.Context, channelID, personaID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithImagesAndContext(ctx, channelID, personaID, userMessage, userMessage, images, nil)
}

// HandleMessageStreamWithImagesByConversationID processes a multimodal message for a specific conversation session.
func (s *ChatService) HandleMessageStreamWithImagesByConversationID(ctx context.Context, conversationID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithImagesAndContextInternal(ctx, "", "", conversationID, userMessage, userMessage, images, nil)
}

// HandleMessageStreamWithImagesAndContext processes a user message with images and separate display/storage versions.
// contextForLLM: the full message (with channel history) sent to LLM — also used for persistence
// messageToStore: the clean message (without channel history markers) saved to conversation store
// images: direct attachment images — persisted to disk and sent to LLM
// historyEntries: structured channel history entries with per-message images (ephemeral, not persisted)
func (s *ChatService) HandleMessageStreamWithImagesAndContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	return s.handleMessageStreamWithImagesAndContextInternal(ctx, channelID, personaID, "", contextForLLM, messageToStore, images, historyEntries)
}

func (s *ChatService) handleMessageStreamWithImagesAndContextInternal(ctx context.Context, channelID, personaID, conversationID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	conv, persona, resolvedPersonaID, err := s.resolveConversationAndPersona(ctx, channelID, personaID, conversationID)
	if err != nil {
		return nil, err
	}
	conv.CumulativeTokens = EstimateConversationContextTokens(conv)

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.executeMemoryFlush(ctx, conv, persona, resolvedPersonaID); err != nil {
			logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
		}
	}

	// Save only direct attachment images to disk (not history entry images)
	var imageRefs []domain.ImageRef
	if s.imageStore != nil && len(images) > 0 {
		for _, img := range images {
			ref, err := s.imageStore.Save(img)
			if err != nil {
				logChat.Warnf("save image %s: %v (skipping)", img.Filename, err)
				continue
			}
			imageRefs = append(imageRefs, ref)
			logChat.Logf("image saved: %s → %s", img.Filename, ref.StorageKey)
		}
	}

	return s.streamWithMultimodalHistory(ctx, conv, persona, resolvedPersonaID, contextForLLM, messageToStore, images, historyEntries, imageRefs...)
}

func (s *ChatService) resolveConversationAndPersona(ctx context.Context, channelID, personaID, conversationID string) (*domain.Conversation, *domain.PersonaModule, string, error) {
	if conversationID != "" {
		conv, err := s.store.GetByID(ctx, conversationID)
		if err != nil {
			return nil, nil, "", fmt.Errorf("get conversation by id: %w", err)
		}
		if conv == nil {
			return nil, nil, "", fmt.Errorf("conversation %q not found", conversationID)
		}
		persona, err := s.resolvePersona(conv.PersonaID)
		if err != nil {
			return nil, nil, "", err
		}
		return conv, persona, conv.PersonaID, nil
	}

	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, nil, "", err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get conversation: %w", err)
	}
	return conv, persona, personaID, nil
}

func (s *ChatService) resolvePersona(personaID string) (*domain.PersonaModule, error) {
	persona, exists := s.personas[personaID]
	if !exists {
		return nil, fmt.Errorf("persona %q not found", personaID)
	}
	return persona, nil
}

// executeMemoryFlush performs a silent turn: asks the agent to save important
// memories via file tools, then compacts history. Invisible to the user.
func (s *ChatService) executeMemoryFlush(ctx context.Context, conv *domain.Conversation, persona *domain.PersonaModule, personaID string) error {
	logChat.Log("executing pre-compaction memory flush...")

	flushPrompt := fmt.Sprintf(
		"SYSTEM: Context window approaching limit. Review this conversation and save important information:\n"+
			"- User facts → user.md (file_write)\n"+
			"- Relationship context, ongoing projects → memory/MEMORY.md (file_write)\n"+
			"- Today's session notes → memory/%s.md (file_append)\n\n"+
			"After updating files (or if nothing needs storing), reply with exactly: NO_REPLY\n"+
			"This message will not be shown to the user.",
		time.Now().Format("2006-01-02"),
	)

	// Temporarily append flush prompt for context assembly
	conv.AppendUserMessage(flushPrompt)
	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return fmt.Errorf("assemble flush context: %w", err)
	}

	// Use streaming to support tool calls during flush
	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return fmt.Errorf("flush LLM call: %w", err)
	}

	// Consume stream, handle tool calls (up to 3 rounds)
	const maxFlushRounds = 3
	var flushResponse string
	var flushTokens int
	for round := 0; round <= maxFlushRounds; round++ {
		var pendingToolCalls []domain.ToolCall
		flushResponse = ""

		for chunk := range streamCh {
			if chunk.TokensUsed > 0 {
				flushTokens += chunk.TokensUsed
			}
			if len(chunk.ToolCalls) > 0 {
				pendingToolCalls = chunk.ToolCalls
				continue
			}
			if chunk.Error != nil {
				conv.Messages = conv.Messages[:len(conv.Messages)-1]
				return fmt.Errorf("flush stream error: %w", chunk.Error)
			}
			flushResponse += chunk.Content
		}

		if len(pendingToolCalls) == 0 {
			break
		}
		if round == maxFlushRounds {
			break
		}

		// Execute tools (file_write, file_append)
		names := make([]string, len(pendingToolCalls))
		for i, c := range pendingToolCalls {
			names[i] = c.Name
		}
		logChat.Logf("flush tool calls: %s", strings.Join(names, ", "))

		toolResults := s.executeTools(ctx, pendingToolCalls, personaID)
		toolResultsText := formatToolResults(pendingToolCalls, toolResults)

		// Append tool exchange to conversation for next round
		conv.AppendAssistantMessage("[Memory flush tool calls]")
		conv.Messages = append(conv.Messages, domain.Message{
			Role:    domain.RoleUser,
			Content: toolResultsText,
		})

		// Continue conversation with tool results
		assembled, err = s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
		if err != nil {
			break
		}
		streamCh, err = s.provider.StreamChat(ctx, llm.ChatRequest{
			SystemPrompt: assembled.SystemPrompt,
			Messages:     assembled.Messages,
			Generation:   assembled.Generation,
			ToolMode:     llm.ToolModeNormal,
		})
		if err != nil {
			break
		}
	}

	// Remove all flush-related messages from history (invisible to user)
	// Find and remove everything after the original conversation
	// The flush prompt was appended, so we need to remove it and any tool exchanges
	msgCount := len(conv.Messages)
	// Walk backwards to find the flush prompt (first message we added)
	for i := msgCount - 1; i >= 0; i-- {
		if conv.Messages[i].Content == flushPrompt {
			conv.Messages = conv.Messages[:i]
			break
		}
	}

	// Check response
	trimmed := strings.TrimSpace(flushResponse)
	if trimmed == "NO_REPLY" || trimmed == "[NO_REPLY]" {
		logChat.Log("flush complete: no memories to store")
	} else {
		logChat.Logf("flush complete: agent responded (%d chars)", len(trimmed))
	}

	// Compact history
	s.contextManager.CompactHistory(conv, persona)
	conv.AddUsageTokens(flushTokens)
	if flushTokens > 0 {
		logChat.Logf("flush tokens: +%d (usage_total: %d, context_estimate: %d)", flushTokens, conv.UsageTokensTotal, conv.CumulativeTokens)
	}

	// Persist compacted conversation
	if err := s.store.Save(ctx, conv); err != nil {
		return fmt.Errorf("save after flush: %w", err)
	}

	logChat.Log("memory flush and compaction done")
	return nil
}

// executeTools executes a list of tool calls and returns the results
func (s *ChatService) executeTools(ctx context.Context, toolCalls []domain.ToolCall, personaID string) []domain.ToolResult {
	results := make([]domain.ToolResult, 0, len(toolCalls))

	for _, call := range toolCalls {
		targetSummary := summarizeToolTarget(call, personaID)
		tool, err := s.resolveTool(call.Name, personaID)
		if err != nil {
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    err.Error(),
				IsError:    true,
			})
			logChat.Warnf("tool %s failed%s: %s", call.Name, targetSummary, truncateLog(err.Error(), 140))
			continue
		}

		// Execute tool
		logChat.Logf("executing: %s%s", call.Name, targetSummary)
		result, err := tool.Execute(ctx, call.Input)
		if err != nil {
			logChat.Warnf("tool %s error%s: %v", call.Name, targetSummary, err)
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error executing tool: %v", err),
				IsError:    true,
			})
			continue
		}

		if result.IsError {
			logChat.Warnf("tool %s failed%s: %s", call.Name, targetSummary, truncateLog(result.Content, 140))
		} else {
			logChat.Logf("tool %s done%s: %d chars", call.Name, targetSummary, len(result.Content))
		}

		// Success - set tool call ID
		result.ToolCallID = call.ID
		results = append(results, result)
	}

	return results
}

func (s *ChatService) resolveTool(name, personaID string) (domain.Tool, error) {
	if isPersonaFileTool(name) {
		personaDir, err := s.resolvePersonaToolDir(personaID)
		if err != nil {
			return nil, err
		}
		switch name {
		case "file_read":
			return llm.NewFileReadTool(personaDir), nil
		case "file_write":
			return llm.NewFileWriteTool(personaDir), nil
		case "file_append":
			return llm.NewFileAppendTool(personaDir), nil
		}
	}

	tool, exists := s.toolRegistry[name]
	if !exists {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return tool, nil
}

func isPersonaFileTool(name string) bool {
	return name == "file_read" || name == "file_write" || name == "file_append"
}

func (s *ChatService) resolvePersonaToolDir(personaID string) (string, error) {
	if strings.TrimSpace(personaID) == "" {
		return "", fmt.Errorf("persona ID is empty for file tool execution")
	}

	// Primary path: resolve Meta.ID -> loaded persona directory name
	if s != nil && s.personas != nil {
		if persona, ok := s.personas[personaID]; ok && persona != nil {
			dirName := strings.TrimSpace(persona.DirName)
			if dirName == "" {
				dirName = personaID
			}
			personaDir := filepath.Join(personaConfigsBaseDir, dirName)
			info, err := os.Stat(personaDir)
			if err != nil {
				return "", fmt.Errorf("persona %q resolved directory not found: %s (%v)", personaID, personaDir, err)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("persona %q resolved path is not a directory: %s", personaID, personaDir)
			}
			return personaDir, nil
		}
	}

	// Legacy compatibility: allow direct folder-name persona IDs.
	personaDir := filepath.Join(personaConfigsBaseDir, personaID)
	info, err := os.Stat(personaDir)
	if err != nil {
		return "", fmt.Errorf("persona %q directory not found: %s (%v)", personaID, personaDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("persona %q path is not a directory: %s", personaID, personaDir)
	}
	return personaDir, nil
}

func summarizeToolTarget(call domain.ToolCall, personaID string) string {
	if call.Input == nil {
		if isPersonaFileTool(call.Name) {
			return fmt.Sprintf(" (persona=%s)", personaID)
		}
		return ""
	}

	switch call.Name {
	case "file_read", "file_write", "file_append":
		file := ""
		if filename, ok := call.Input["filename"].(string); ok && filename != "" {
			file = filename
		} else if filename, ok := call.Input["file"].(string); ok && filename != "" {
			file = filename
		}
		if file != "" {
			return fmt.Sprintf(" (persona=%s, file=%s)", personaID, file)
		}
		return fmt.Sprintf(" (persona=%s)", personaID)
	case "web_fetch":
		if url, ok := call.Input["url"].(string); ok && url != "" {
			return fmt.Sprintf(" (%s)", truncateLog(url, 80))
		}
	case "web_search":
		if query, ok := call.Input["query"].(string); ok && query != "" {
			return fmt.Sprintf(" (%s)", truncateLog(query, 80))
		}
	}
	return ""
}

func toolCallSignature(call domain.ToolCall) string {
	if call.Input == nil {
		return call.Name + "|{}"
	}
	b, err := json.Marshal(call.Input)
	if err != nil {
		return fmt.Sprintf("%s|%v", call.Name, call.Input)
	}
	return call.Name + "|" + string(b)
}

type toolGuardKind string

const (
	toolGuardKindDuplicate   toolGuardKind = "duplicate_call"
	toolGuardKindLimit       toolGuardKind = "tool_limit"
	toolGuardKindMaxRounds   toolGuardKind = "max_tool_rounds"
	toolGuardKindForcedFinal toolGuardKind = "forced_final_answer"
)

type toolGuardEvent struct {
	Kind    toolGuardKind
	Tool    string
	Reason  string
	Details string
}

type toolGuardSummary struct {
	Events []toolGuardEvent
}

func (s *toolGuardSummary) Add(kind toolGuardKind, toolName, reason, details string) {
	if s == nil {
		return
	}
	s.Events = append(s.Events, toolGuardEvent{
		Kind:    kind,
		Tool:    toolName,
		Reason:  reason,
		Details: details,
	})
}

func (s toolGuardSummary) Empty() bool {
	return len(s.Events) == 0
}

func (s toolGuardSummary) Has(kind toolGuardKind) bool {
	for _, event := range s.Events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func (s toolGuardSummary) shouldForceFinalAnswer(policy ToolExecutionPolicy) bool {
	if !policy.ForceFinalAnswerAfterToolLimit {
		return false
	}
	return s.Has(toolGuardKindMaxRounds) || s.Has(toolGuardKindLimit) || s.Has(toolGuardKindDuplicate)
}

func (s toolGuardSummary) buildPromptText() string {
	if len(s.Events) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tool guard events in this turn:\n")
	for i, event := range s.Events {
		tool := event.Tool
		if strings.TrimSpace(tool) == "" {
			tool = "unknown"
		}
		b.WriteString(fmt.Sprintf("%d. kind=%s tool=%s reason=%s", i+1, event.Kind, tool, event.Reason))
		if strings.TrimSpace(event.Details) != "" {
			b.WriteString(" details=")
			b.WriteString(event.Details)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func buildToolLoopFallbackResponse(policy PromptGuardPolicy) string {
	if policy.TripletOutputEnabled {
		return strings.TrimSpace(
			"[Status]: Degraded / Tool Loop Stopped\n" +
				"[Analysis]: Repeated tool calls were blocked to prevent infinite recursion.\n" +
				"[Response]: 抱歉，我剛剛在工具呼叫流程卡住了，已自動停止重複呼叫。請再說一次你的需求，我會直接回覆重點，不再重複呼叫工具。",
		)
	}
	return "抱歉，我剛剛在工具呼叫流程卡住了，已自動停止重複呼叫。請再說一次你的需求，我會直接回覆重點。"
}

func buildDuplicateToolGuardResult(call domain.ToolCall) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Content: fmt.Sprintf(
			"Tool guard: duplicate call blocked for %q with identical input in this turn. "+
				"Do NOT call it again; continue with available results and answer the user directly.",
			call.Name,
		),
		IsError: true,
	}
}

func buildToolLimitGuardResult(call domain.ToolCall, limit int) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Content: fmt.Sprintf(
			"Tool guard: %q call limit reached (%d per turn). "+
				"Do NOT call this tool again in this turn; continue with available results and answer directly.",
			call.Name,
			limit,
		),
		IsError: true,
	}
}

func buildForcedFinalAnswerPrompt(summary toolGuardSummary) string {
	var b strings.Builder
	b.WriteString("[Tool guard continuation]\n")
	b.WriteString("Tool execution was intentionally limited to prevent loops. ")
	b.WriteString("You MUST provide the best possible final answer using only already available tool results and conversation context.\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT call any tool again.\n")
	b.WriteString("- If evidence is partial, state uncertainty briefly and still provide a concrete answer.\n")
	b.WriteString("- Do not ask the user to repeat unless absolutely necessary.\n")
	if details := summary.buildPromptText(); details != "" {
		b.WriteString("\n")
		b.WriteString(details)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *ChatService) forceFinalAnswerWithoutTools(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	summary toolGuardSummary,
) (string, int, error) {
	forcedPrompt := buildForcedFinalAnswerPrompt(summary)
	conv.Messages = append(conv.Messages, domain.Message{
		Role:    domain.RoleUser,
		Content: forcedPrompt,
	})
	defer func() {
		if len(conv.Messages) > 0 {
			conv.Messages = conv.Messages[:len(conv.Messages)-1]
		}
	}()

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return "", 0, fmt.Errorf("assemble forced final answer: %w", err)
	}
	s.logPromptSnapshot("stream-forced-final", conv.ChannelID, conv.PersonaID, conv.ID, assembled)

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeDisabled,
	})
	if err != nil {
		return "", 0, fmt.Errorf("forced final answer llm call: %w", err)
	}

	var fullResponse strings.Builder
	var tokensUsed int
	for chunk := range streamCh {
		if chunk.TokensUsed > 0 {
			tokensUsed += chunk.TokensUsed
		}
		if chunk.Error != nil {
			return "", tokensUsed, chunk.Error
		}
		if len(chunk.ToolCalls) > 0 {
			return "", tokensUsed, fmt.Errorf("forced final answer unexpectedly requested tool calls")
		}
		fullResponse.WriteString(chunk.Content)
	}

	return strings.TrimSpace(fullResponse.String()), tokensUsed, nil
}

// handleToolCallingStream handles the tool calling loop with streaming.
// When the LLM requests tool calls, this executes them and continues the conversation.
// Supports up to maxToolRounds of chained tool calls.
// multimodalHistory is non-nil when the conversation includes images; the continuation
// will use StreamChatMultimodal to preserve image context.
func (s *ChatService) handleToolCallingStream(
	ctx context.Context,
	streamCh <-chan llm.StreamChunk,
	conv *domain.Conversation,
	messageToStore, contextForLLM string,
	persona *domain.PersonaModule,
	personaID string,
	rawLLMInput string,
	imageRefs []domain.ImageRef, // nil for text-only messages
	multimodalHistory []domain.MultimodalMessage, // nil for text-only; includes images for multimodal
) <-chan llm.StreamChunk {
	outputCh := make(chan llm.StreamChunk, 16)

	go func() {
		defer close(outputCh)

		toolPolicy := s.toolExecutionPolicySnapshot()
		var executedToolCalls []domain.ToolCall
		var allToolResultsText string
		var totalTokens int
		toolCallAttempts := make(map[string]int)
		toolCountsByName := make(map[string]int)
		emittedToolErrors := make(map[string]struct{})
		hadToolCallAttempt := false
		guardSummary := toolGuardSummary{}
		promptPolicy := s.assembler.PromptGuardPolicy()
		hideAnalysis := promptPolicy.TripletOutputEnabled && promptPolicy.AnalysisVisibility == AnalysisVisibilityHidden

		currentStream := streamCh
		var fullResponse string
		finalTextSent := false
		baseMessageCount := len(conv.Messages)
		conv.AppendUserMessage(contextForLLM)
		defer func() {
			if baseMessageCount <= len(conv.Messages) {
				conv.Messages = conv.Messages[:baseMessageCount]
			}
		}()

		for round := 0; round <= toolPolicy.MaxToolRounds; round++ {
			var pendingToolCalls []domain.ToolCall
			fullResponse = ""
			var visibleRoundContent strings.Builder

			// Consume stream
			for chunk := range currentStream {
				if chunk.TokensUsed > 0 {
					totalTokens += chunk.TokensUsed
				}
				if len(chunk.ToolCalls) > 0 {
					pendingToolCalls = chunk.ToolCalls
					outputCh <- chunk
					continue
				}
				if chunk.Error != nil {
					outputCh <- chunk
					return
				}
				fullResponse += chunk.Content
				if !hideAnalysis && chunk.Content != "" {
					visibleRoundContent.WriteString(chunk.Content)
				}
			}

			// No tool calls → we have the final response
			if len(pendingToolCalls) == 0 {
				if !hideAnalysis && visibleRoundContent.Len() > 0 {
					outputCh <- llm.StreamChunk{Content: visibleRoundContent.String()}
					finalTextSent = true
				}
				break
			}

			// Log tool call names
			names := make([]string, len(pendingToolCalls))
			for i, c := range pendingToolCalls {
				names[i] = c.Name
			}
			logChat.Logf("tool calls (round %d/%d): %s", round+1, toolPolicy.MaxToolRounds, strings.Join(names, ", "))
			hadToolCallAttempt = true

			// Max rounds reached → force finalize on next step.
			if round == toolPolicy.MaxToolRounds {
				guardSummary.Add(toolGuardKindMaxRounds, "", "max_tool_rounds_reached",
					fmt.Sprintf("max_tool_rounds=%d", toolPolicy.MaxToolRounds))
				atomic.AddUint64(&s.metricToolGuardMaxRounds, 1)
				logChat.Warnf("max tool rounds (%d) reached, stopping", toolPolicy.MaxToolRounds)
				break
			}

			// Send tool execution start status for each tool
			for _, call := range pendingToolCalls {
				outputCh <- llm.StreamChunk{
					ToolStatus: "tool_start",
					ToolName:   call.Name,
					ToolInput:  call.Input,
				}
			}

			// Execute tools with duplicate-call guard to prevent endless loops.
			executableCalls := make([]domain.ToolCall, 0, len(pendingToolCalls))
			toolResults := make([]domain.ToolResult, 0, len(pendingToolCalls))
			for _, call := range pendingToolCalls {
				if call.Name == "web_search" && toolCountsByName[call.Name] >= toolPolicy.MaxWebSearchCallsPerTurn {
					guardSummary.Add(
						toolGuardKindLimit,
						call.Name,
						"tool_call_limit_reached",
						fmt.Sprintf("max_web_search_calls_per_turn=%d", toolPolicy.MaxWebSearchCallsPerTurn),
					)
					atomic.AddUint64(&s.metricToolGuardLimitBlocked, 1)
					toolResults = append(toolResults, buildToolLimitGuardResult(call, toolPolicy.MaxWebSearchCallsPerTurn))
					logChat.Warnf("tool loop guard blocked limit (round %d): %s%s", round+1, call.Name, summarizeToolTarget(call, personaID))
					continue
				}

				signature := toolCallSignature(call)
				if toolCallAttempts[signature] >= 1 {
					guardSummary.Add(toolGuardKindDuplicate, call.Name, "duplicate_tool_call_blocked", "")
					atomic.AddUint64(&s.metricToolGuardDuplicateBlocked, 1)
					toolResults = append(toolResults, buildDuplicateToolGuardResult(call))
					logChat.Warnf("tool loop guard blocked duplicate call (round %d): %s%s", round+1, call.Name, summarizeToolTarget(call, personaID))
					continue
				}
				toolCallAttempts[signature]++
				toolCountsByName[call.Name]++
				executableCalls = append(executableCalls, call)
			}

			if len(executableCalls) > 0 {
				executed := s.executeTools(ctx, executableCalls, personaID)
				toolResults = append(toolResults, executed...)
				executedToolCalls = append(executedToolCalls, executableCalls...)
			}
			toolCallMap := make(map[string]domain.ToolCall, len(pendingToolCalls))
			for _, call := range pendingToolCalls {
				toolCallMap[call.ID] = call
			}
			for _, result := range toolResults {
				if !result.IsError {
					continue
				}
				call := toolCallMap[result.ToolCallID]
				if toolPolicy.DiscordShowInternalToolErrors {
					errSummary := truncateLog(result.Content, 240)
					dedupeKey := call.Name + "|" + toolCallSignature(call) + "|" + errSummary
					if _, exists := emittedToolErrors[dedupeKey]; exists {
						continue
					}
					emittedToolErrors[dedupeKey] = struct{}{}
					outputCh <- llm.StreamChunk{
						ToolStatus: "tool_error",
						ToolName:   call.Name,
						ToolInput:  call.Input,
						ToolError:  errSummary,
					}
				}
			}

			// Send tool execution done status
			outputCh <- llm.StreamChunk{ToolStatus: "tool_done"}

			// Build tool results message for LLM
			toolResultsText := formatToolResults(pendingToolCalls, toolResults)
			allToolResultsText += toolResultsText

			// Append assistant's tool call request to conversation
			conv.AppendAssistantMessage("[Tool calls executed]")

			// Append tool results as user message
			conv.Messages = append(conv.Messages, domain.Message{
				Role:    domain.RoleUser,
				Content: toolResultsText,
			})

			// Also append to multimodal history if in multimodal mode
			if multimodalHistory != nil {
				multimodalHistory = append(multimodalHistory,
					domain.MultimodalMessage{
						Role:  domain.RoleAssistant,
						Parts: []domain.MessagePart{{Text: "[Tool calls executed]"}},
					},
					domain.MultimodalMessage{
						Role:  domain.RoleUser,
						Parts: []domain.MessagePart{{Text: toolResultsText}},
					},
				)
			}

			// Continue conversation with tool results
			logChat.Logf("continuing with tool results → LLM (round %d)", round+1)
			assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("assemble context after tools: %w", err), Done: true}
				return
			}
			s.logPromptSnapshot("stream-continuation", conv.ChannelID, conv.PersonaID, conv.ID, assembled)

			// Use multimodal continuation when images are present to preserve image context
			if multimodalHistory != nil {
				currentStream, err = s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
			} else {
				currentStream, err = s.provider.StreamChat(ctx, llm.ChatRequest{
					SystemPrompt: assembled.SystemPrompt,
					Messages:     assembled.Messages,
					Generation:   assembled.Generation,
					ToolMode:     llm.ToolModeNormal,
				})
			}
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("llm continuation: %w", err), Done: true}
				return
			}
		}

		if strings.TrimSpace(fullResponse) == "" && hadToolCallAttempt && guardSummary.shouldForceFinalAnswer(toolPolicy) {
			guardSummary.Add(toolGuardKindForcedFinal, "", "tool_guard_forced_final_answer", "")
			forcedResponse, forcedTokens, err := s.forceFinalAnswerWithoutTools(ctx, conv, persona, guardSummary)
			totalTokens += forcedTokens
			if err != nil {
				atomic.AddUint64(&s.metricToolForcedFinalFailed, 1)
				logChat.Warnf("forced final answer failed: %v", err)
			} else if strings.TrimSpace(forcedResponse) != "" {
				atomic.AddUint64(&s.metricToolForcedFinalSuccess, 1)
				fullResponse = forcedResponse
				if !hideAnalysis {
					outputCh <- llm.StreamChunk{Content: forcedResponse}
					finalTextSent = true
				}
				logChat.Logf("tool loop forced final answer generated after %d tool call(s)", len(executedToolCalls))
			}
		}

		if strings.TrimSpace(fullResponse) == "" && hadToolCallAttempt {
			atomic.AddUint64(&s.metricToolLoopFallback, 1)
			fullResponse = buildToolLoopFallbackResponse(promptPolicy)
			logChat.Warnf("tool loop fallback response generated after %d tool call(s)", len(executedToolCalls))
		}

		finalView := s.prepareAssistantOutputForStorage(fullResponse)

		if hideAnalysis && finalView.Display != "" {
			outputCh <- llm.StreamChunk{Content: finalView.Display}
			finalTextSent = true
		}
		if !hideAnalysis && !finalTextSent && finalView.Display != "" {
			outputCh <- llm.StreamChunk{Content: finalView.Display}
			finalTextSent = true
		}

		// Save conversation and emit final signals
		if len(executedToolCalls) > 0 {
			// Tool calls were executed — append annotation and save
			toolUsageAnnotation := formatToolUsageAnnotation(executedToolCalls)
			display := finalView.Display
			if display == "" {
				display = finalView.Content
			}
			finalResponseWithAnnotation := display + "\n\n" + toolUsageAnnotation

			// Stream the annotation as a separate chunk
			outputCh <- llm.StreamChunk{Content: "\n\n" + toolUsageAnnotation}

			// Store final conversation with raw LLM data
			rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s",
				allToolResultsText, fullResponse)
			if baseMessageCount <= len(conv.Messages) {
				conv.Messages = conv.Messages[:baseMessageCount]
			}
			if len(imageRefs) > 0 {
				conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
			} else {
				conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			}
			content := finalView.Content
			if content == "" {
				content = strings.TrimSpace(fullResponse)
			}
			conv.AppendAssistantMessageWithDisplayAndRaw(content, finalResponseWithAnnotation, rawLLMInput, rawLLMOutput)
			s.applyTokenAccounting(conv, totalTokens)
			if err := s.store.Save(ctx, conv); err != nil {
				logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
			}

		} else {
			// No tool calls - store conversation normally with raw LLM data
			if fullResponse != "" || messageToStore != "" {
				if baseMessageCount <= len(conv.Messages) {
					conv.Messages = conv.Messages[:baseMessageCount]
				}
				if len(imageRefs) > 0 {
					conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
				} else {
					conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
				}
				conv.AppendAssistantMessageWithDisplayAndRaw(finalView.Content, finalView.Display, rawLLMInput, finalView.RawOutput)
				s.applyTokenAccounting(conv, totalTokens)
				if err := s.store.Save(ctx, conv); err != nil {
					logChat.Errorf("save conversation failed [%s/%s]: %v", conv.ChannelID, conv.PersonaID, err)
				}
			}
		}

		// Send final Done signal to indicate stream completion
		outputCh <- llm.StreamChunk{Done: true}
	}()

	return outputCh
}

// streamWithMultimodalHistory builds a multimodal history (loading images from disk for recent turns)
// and streams the response. `currentImages` are the images for the current message (may be nil for text-only follow-ups).
// `currentImageRefs` are the refs to persist in the conversation JSON.
func (s *ChatService) streamWithMultimodalHistory(
	ctx context.Context,
	conv *domain.Conversation,
	persona *domain.PersonaModule,
	personaID string,
	contextForLLM, messageToStore string,
	currentImages []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
	currentImageRefs ...domain.ImageRef,
) (<-chan llm.StreamChunk, error) {
	// Assemble budgeted text context first, including the current user turn for digest relevance.
	conv.AppendUserMessage(contextForLLM)
	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return nil, fmt.Errorf("assemble context: %w", err)
	}
	s.logPromptSnapshot("multimodal-stream", conv.ChannelID, personaID, conv.ID, assembled)
	conv.Messages = conv.Messages[:len(conv.Messages)-1]

	// Convert assembler output to multimodal text history and drop the temporary current turn.
	baseMessages := assembled.Messages
	if n := len(baseMessages); n > 0 {
		last := baseMessages[n-1]
		if last.Role == domain.RoleUser && last.Content == contextForLLM {
			baseMessages = baseMessages[:n-1]
		}
	}
	multimodalHistory := buildTextMultimodalHistory(baseMessages)

	// Build current user message parts with interleaved history images
	var userParts []domain.MessagePart
	if len(historyEntries) > 0 {
		// History header
		userParts = append(userParts, domain.MessagePart{Text: domain.HistoryContextMarker})
		// Each history entry: text part followed by its associated images
		for _, entry := range historyEntries {
			userParts = append(userParts, domain.MessagePart{Text: entry.FormattedText})
			for i := range entry.Images {
				logChat.Logf("history image: %s (%s, %.1fKB)", entry.Images[i].Filename, entry.Images[i].MimeType, float64(len(entry.Images[i].Data))/1024)
				userParts = append(userParts, domain.MessagePart{ImageData: &entry.Images[i]})
			}
		}
		// Current message section
		userParts = append(userParts, domain.MessagePart{Text: "\n" + domain.CurrentMessageMarker + "\n" + messageToStore})
	} else {
		// No history entries (DM or text-only history) — use flat context
		userParts = append(userParts, domain.MessagePart{Text: contextForLLM})
	}
	// Direct attachment images (always after current message text)
	for i := range currentImages {
		logChat.Logf("image: %s (%s, %.1fKB)", currentImages[i].Filename, currentImages[i].MimeType, float64(len(currentImages[i].Data))/1024)
		userParts = append(userParts, domain.MessagePart{ImageData: &currentImages[i]})
	}

	// Append current user message
	multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
		Role:  domain.RoleUser,
		Parts: userParts,
	})

	totalImages := len(currentImages)
	for _, mm := range multimodalHistory {
		for _, p := range mm.Parts {
			if p.ImageData != nil {
				totalImages++
			}
		}
	}
	logChat.Logf("multimodal history: %d messages, %d total images → LLM", len(multimodalHistory), totalImages)

	// Build raw LLM input for transparency
	var textOnlyMessages []domain.Message
	for _, mm := range multimodalHistory {
		var textParts []string
		for _, part := range mm.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.ImageData != nil {
				textParts = append(textParts, fmt.Sprintf("[Image: %s]", part.ImageData.Filename))
			}
		}
		textOnlyMessages = append(textOnlyMessages, domain.Message{
			Role:    mm.Role,
			Content: strings.Join(textParts, "\n"),
		})
	}
	rawLLMInput := formatLLMPrompt(assembled.SystemPrompt, textOnlyMessages)

	streamCh, err := s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
	if err != nil {
		return nil, fmt.Errorf("llm multimodal stream: %w", err)
	}

	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, personaID, rawLLMInput, currentImageRefs, multimodalHistory), nil
}

func buildTextMultimodalHistory(messages []domain.Message) []domain.MultimodalMessage {
	result := make([]domain.MultimodalMessage, 0, len(messages))
	for _, msg := range messages {
		result = append(result, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     []domain.MessagePart{{Text: msg.Content}},
			Timestamp: msg.Timestamp,
		})
	}
	return result
}

func (s *ChatService) logPromptSnapshot(path, channelID, personaID, conversationID string, assembled *AssembledContext) {
	if assembled == nil {
		return
	}
	systemChars := len(assembled.SystemPrompt)
	systemInjected := strings.TrimSpace(assembled.SystemPrompt) != ""
	personaLock := hasPersonaLockMessage(assembled.Messages)
	logChat.Logf("prompt[%s]: system_injected=%t, system_chars=%d, messages=%d, persona_lock=%t, channel=%s, persona=%s, conversation=%s",
		path, systemInjected, systemChars, len(assembled.Messages), personaLock, channelID, personaID, conversationID)
}

func hasPersonaLockMessage(messages []domain.Message) bool {
	for _, msg := range messages {
		if msg.Role != domain.RoleUser {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "[Persona lock]") {
			return true
		}
	}
	return false
}

// formatToolResults formats tool results for LLM consumption
func formatToolResults(toolCalls []domain.ToolCall, results []domain.ToolResult) string {
	var formatted string
	formatted += "Tool execution results:\n\n"

	// Create a map for quick lookup of tool calls by ID
	callMap := make(map[string]domain.ToolCall)
	for _, call := range toolCalls {
		callMap[call.ID] = call
	}

	for _, result := range results {
		call, exists := callMap[result.ToolCallID]
		toolName := "unknown"
		if exists {
			toolName = call.Name
		}

		compressed := compressToolResult(toolName, result.Content)
		formatted += fmt.Sprintf("Tool: %s (ID: %s)\n", toolName, result.ToolCallID)
		if result.IsError {
			formatted += fmt.Sprintf("Status: ERROR\n%s\n\n", compressed)
		} else {
			formatted += fmt.Sprintf("Status: SUCCESS\n%s\n\n", compressed)
		}
	}
	return formatted
}

func compressToolResult(toolName, content string) string {
	if content == "" {
		return content
	}

	maxChars := 6000
	switch toolName {
	case "web_fetch":
		// Keep metadata and a smaller excerpt of content body.
		maxChars = 3500
		if start := strings.Index(content, "--- CONTENT START ---"); start > 0 {
			header := content[:start]
			body := content[start:]
			body = truncateLog(body, 2200)
			return header + body + "\n\n[tool output truncated for token efficiency]"
		}
	case "web_search":
		maxChars = 3000
	case "discord_observe":
		maxChars = 3000
	case "file_read":
		maxChars = 2500
	}

	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "\n\n[tool output truncated for token efficiency]"
}

// formatToolUsageAnnotation creates a user-friendly annotation for tool usage
func formatToolUsageAnnotation(toolCalls []domain.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	var annotation strings.Builder
	annotation.WriteString("---\n")
	annotation.WriteString("🔧 **使用的工具**：\n")

	for i, call := range toolCalls {
		// Format tool name
		toolEmoji := "🔧"
		toolDisplayName := call.Name

		switch call.Name {
		case "web_search":
			toolEmoji = "🔍"
			toolDisplayName = "網路搜尋"
			if query, ok := call.Input["query"].(string); ok {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**：`%s`\n", i+1, toolEmoji, toolDisplayName, query))
			} else {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**\n", i+1, toolEmoji, toolDisplayName))
			}
		case "web_fetch":
			toolEmoji = "📄"
			toolDisplayName = "網頁擷取"
			if url, ok := call.Input["url"].(string); ok {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**：`%s`\n", i+1, toolEmoji, toolDisplayName, url))
			} else {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**\n", i+1, toolEmoji, toolDisplayName))
			}
		default:
			annotation.WriteString(fmt.Sprintf("%d. %s **%s**\n", i+1, toolEmoji, toolDisplayName))
		}
	}

	return annotation.String()
}

// formatLLMPrompt builds a complete LLM prompt string for debugging/transparency
func formatLLMPrompt(systemPrompt string, messages []domain.Message) string {
	var prompt strings.Builder

	prompt.WriteString("=== SYSTEM PROMPT ===\n")
	prompt.WriteString(systemPrompt)
	prompt.WriteString("\n\n=== CONVERSATION HISTORY ===\n")

	for i, msg := range messages {
		prompt.WriteString(fmt.Sprintf("\n[Message %d - %s]\n", i+1, msg.Role))
		prompt.WriteString(msg.Content)
		prompt.WriteString("\n")
	}

	return prompt.String()
}

func (s *ChatService) prepareAssistantOutputForStorage(raw string) assistantOutputView {
	policy := s.assembler.PromptGuardPolicy()
	view := prepareAssistantOutput(raw, policy)
	if policy.TripletOutputEnabled && strings.TrimSpace(raw) != "" && !view.Parsed {
		count := atomic.AddUint64(&tripletParseFailedCount, 1)
		logChat.Warnf("metric prompt_guard.triplet_parse_failed=%d", count)
	}
	if view.Content == "" {
		view.Content = strings.TrimSpace(raw)
	}
	if view.Display == "" {
		view.Display = view.Content
	}
	if view.RawOutput == "" {
		view.RawOutput = strings.TrimSpace(raw)
	}
	return view
}

func (s *ChatService) applyTokenAccounting(conv *domain.Conversation, turnTokens int) {
	if conv == nil {
		return
	}
	conv.AddUsageTokens(turnTokens)
	conv.CumulativeTokens = EstimateConversationContextTokens(conv)

	if turnTokens > 0 {
		logChat.Logf("tokens: +%d (usage_total: %d, context_estimate: %d)", turnTokens, conv.UsageTokensTotal, conv.CumulativeTokens)
	}
}

// truncateLog truncates a string for log output
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
