package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logChat = logger.New("chat", logger.Cyan)

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
	}
}

// HandleMessage processes a user message and returns the assistant response.
func (s *ChatService) HandleMessage(ctx context.Context, channelID, personaID, userMessage string) (string, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return "", err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return "", fmt.Errorf("get conversation: %w", err)
	}

	conv.AppendUserMessage(userMessage)

	modelInfo := s.provider.ModelInfo()
	fmt.Printf("[ChatService] Assembling context - Provider: %s, SupportsThinking: %v\n",
		modelInfo.ProviderName, modelInfo.SupportsThinking)

	assembled, err := s.assembler.Assemble(persona, conv, modelInfo)
	if err != nil {
		return "", fmt.Errorf("assemble context: %w", err)
	}

	fmt.Printf("[ChatService] Calling LLM - System prompt length: %d, Messages: %d\n",
		len(assembled.SystemPrompt), len(assembled.Messages))

	resp, err := s.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	fmt.Printf("[ChatService] LLM response received - Length: %d chars\n", len(resp.Content))

	conv.AppendAssistantMessage(resp.Content)

	if err := s.store.Save(ctx, conv); err != nil {
		return "", fmt.Errorf("save conversation: %w", err)
	}

	return resp.Content, nil
}

// HandleMessageStream processes a user message and returns a streaming channel.
func (s *ChatService) HandleMessageStream(ctx context.Context, channelID, personaID, userMessage string) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithContext(ctx, channelID, personaID, userMessage, userMessage)
}

// HandleMessageStreamWithContext processes a user message with separate display and storage versions.
// contextForLLM: the full message (with channel history) sent to LLM
// messageToStore: the clean message (without channel history markers) saved to conversation store
func (s *ChatService) HandleMessageStreamWithContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string) (<-chan llm.StreamChunk, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.executeMemoryFlush(ctx, conv, persona); err != nil {
			logChat.Warnf("memory flush failed: %v (continuing anyway)", err)
		}
	}

	// Temporarily append the LLM context version for assembly
	conv.AppendUserMessage(contextForLLM)

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

	// Remove the temporary message - we'll store the clean version later
	conv.Messages = conv.Messages[:len(conv.Messages)-1]

	// Build raw LLM input for transparency (before streaming)
	rawLLMInput := formatLLMPrompt(assembled.SystemPrompt, assembled.Messages)

	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
	})
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	// Handle tool calling loop and wrap stream for persistence
	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, personaID, rawLLMInput, nil, nil), nil
}

// HandleMessageStreamWithImages processes a user message with images and returns a streaming channel.
func (s *ChatService) HandleMessageStreamWithImages(ctx context.Context, channelID, personaID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithImagesAndContext(ctx, channelID, personaID, userMessage, userMessage, images, nil)
}

// HandleMessageStreamWithImagesAndContext processes a user message with images and separate display/storage versions.
// contextForLLM: the full message (with channel history) sent to LLM — also used for persistence
// messageToStore: the clean message (without channel history markers) saved to conversation store
// images: direct attachment images — persisted to disk and sent to LLM
// historyEntries: structured channel history entries with per-message images (ephemeral, not persisted)
func (s *ChatService) HandleMessageStreamWithImagesAndContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string, images []domain.ImageInput, historyEntries []domain.ChannelHistoryEntry) (<-chan llm.StreamChunk, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Pre-flight: flush memory and compact history if approaching context limit
	if s.contextManager != nil && s.contextManager.ShouldFlush(conv) {
		if err := s.executeMemoryFlush(ctx, conv, persona); err != nil {
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

	return s.streamWithMultimodalHistory(ctx, conv, persona, personaID, contextForLLM, messageToStore, images, historyEntries, imageRefs...)
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
func (s *ChatService) executeMemoryFlush(ctx context.Context, conv *domain.Conversation, persona *domain.PersonaModule) error {
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
	})
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return fmt.Errorf("flush LLM call: %w", err)
	}

	// Consume stream, handle tool calls (up to 3 rounds)
	const maxFlushRounds = 3
	var flushResponse string
	for round := 0; round <= maxFlushRounds; round++ {
		var pendingToolCalls []domain.ToolCall
		flushResponse = ""

		for chunk := range streamCh {
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

		toolResults := s.executeTools(ctx, pendingToolCalls)
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
	s.contextManager.CompactHistory(conv)

	// Persist compacted conversation
	if err := s.store.Save(ctx, conv); err != nil {
		return fmt.Errorf("save after flush: %w", err)
	}

	logChat.Log("memory flush and compaction done")
	return nil
}

// executeTools executes a list of tool calls and returns the results
func (s *ChatService) executeTools(ctx context.Context, toolCalls []domain.ToolCall) []domain.ToolResult {
	results := make([]domain.ToolResult, 0, len(toolCalls))

	for _, call := range toolCalls {
		tool, exists := s.toolRegistry[call.Name]
		if !exists {
			// Tool not found - return error result
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error: tool %q not found", call.Name),
				IsError:    true,
			})
			continue
		}

		// Execute tool
		logChat.Logf("executing: %s", call.Name)
		result, err := tool.Execute(ctx, call.Input)
		if err != nil {
			logChat.Warnf("tool %s error: %v", call.Name, err)
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error executing tool: %v", err),
				IsError:    true,
			})
			continue
		}

		if result.IsError {
			logChat.Warnf("tool %s failed: %s", call.Name, truncateLog(result.Content, 100))
		} else {
			logChat.Logf("tool %s done: %d chars", call.Name, len(result.Content))
		}

		// Success - set tool call ID
		result.ToolCallID = call.ID
		results = append(results, result)
	}

	return results
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

		const maxToolRounds = 3
		var allToolCalls []domain.ToolCall
		var allToolResultsText string
		var totalTokens int

		currentStream := streamCh
		var fullResponse string

		for round := 0; round <= maxToolRounds; round++ {
			var pendingToolCalls []domain.ToolCall
			fullResponse = ""

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
				outputCh <- chunk
				if chunk.Error != nil {
					return
				}
				fullResponse += chunk.Content
			}

			// No tool calls → we have the final response
			if len(pendingToolCalls) == 0 {
				break
			}

			// Max rounds reached → stop and use whatever we have
			if round == maxToolRounds {
				logChat.Warnf("max tool rounds (%d) reached, stopping", maxToolRounds)
				break
			}

			// Log tool call names
			names := make([]string, len(pendingToolCalls))
			for i, c := range pendingToolCalls {
				names[i] = c.Name
			}
			logChat.Logf("tool calls (round %d): %s", round+1, strings.Join(names, ", "))
			allToolCalls = append(allToolCalls, pendingToolCalls...)

			// Send tool execution start status for each tool
			for _, call := range pendingToolCalls {
				outputCh <- llm.StreamChunk{
					ToolStatus: "tool_start",
					ToolName:   call.Name,
					ToolInput:  call.Input,
				}
			}

			// Execute tools
			toolResults := s.executeTools(ctx, pendingToolCalls)

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

			// Use multimodal continuation when images are present to preserve image context
			if multimodalHistory != nil {
				currentStream, err = s.provider.StreamChatMultimodal(ctx, assembled.SystemPrompt, multimodalHistory, assembled.Generation)
			} else {
				currentStream, err = s.provider.StreamChat(ctx, llm.ChatRequest{
					SystemPrompt: assembled.SystemPrompt,
					Messages:     assembled.Messages,
					Generation:   assembled.Generation,
				})
			}
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("llm continuation: %w", err), Done: true}
				return
			}
		}

		// Update token tracking
		if totalTokens > 0 {
			conv.UpdateTokenUsage(totalTokens)
			logChat.Logf("tokens: +%d (cumulative: %d)", totalTokens, conv.CumulativeTokens)
		}

		// Save conversation and emit final signals
		if len(allToolCalls) > 0 {
			// Tool calls were executed — append annotation and save
			toolUsageAnnotation := formatToolUsageAnnotation(allToolCalls)
			finalResponseWithAnnotation := fullResponse + "\n\n" + toolUsageAnnotation

			// Stream the annotation as a separate chunk
			outputCh <- llm.StreamChunk{Content: "\n\n" + toolUsageAnnotation}

			// Store final conversation with raw LLM data
			rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s",
				allToolResultsText, fullResponse)
			if len(imageRefs) > 0 {
				conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
			} else {
				conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			}
			conv.AppendAssistantMessageWithRaw(finalResponseWithAnnotation, rawLLMInput, rawLLMOutput)
			_ = s.store.Save(ctx, conv)

		} else {
			// No tool calls - store conversation normally with raw LLM data
			if fullResponse != "" || messageToStore != "" {
				if len(imageRefs) > 0 {
					conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
				} else {
					conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
				}
				conv.AppendAssistantMessageWithRaw(fullResponse, rawLLMInput, fullResponse)
				_ = s.store.Save(ctx, conv)
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

// truncateLog truncates a string for log output
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
