package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logChat = logger.New("chat", logger.Cyan)

// ChatService orchestrates the full chat pipeline:
// receive message → load conversation → assemble context → call LLM → persist → return.
type ChatService struct {
	provider           llm.Provider
	assembler          *ContextAssembler
	store              store.ConversationRepository
	imageStore         *store.ImageStore // persists images to disk (nil = disabled)
	imageHistoryTurns  int              // how many recent user turns to load images for (default 3)
	personas           map[string]*domain.PersonaModule
	toolRegistry       map[string]domain.Tool // tool name → tool instance
	memoryExtractor    *MemoryExtractor       // automatic post-response memory extraction (nil = disabled)
}

func NewChatService(
	provider llm.Provider,
	assembler *ContextAssembler,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore, // nil = disabled (images won't be persisted)
	imageHistoryTurns int, // 0 → default 3
	personas map[string]*domain.PersonaModule,
	tools []domain.Tool,
	memoryExtractor *MemoryExtractor, // nil = disabled
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
		memoryExtractor:   memoryExtractor,
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

	// If history has recent images, delegate to the multimodal path (no new images, but load from history)
	if s.imageStore != nil && s.historyHasRecentImages(conv) {
		return s.streamWithMultimodalHistory(ctx, conv, persona, personaID, contextForLLM, messageToStore, nil)
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
	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, personaID, rawLLMInput, nil), nil
}

// HandleMessageStreamWithImages processes a user message with images and returns a streaming channel.
func (s *ChatService) HandleMessageStreamWithImages(ctx context.Context, channelID, personaID, userMessage string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	return s.HandleMessageStreamWithImagesAndContext(ctx, channelID, personaID, userMessage, userMessage, images)
}

// HandleMessageStreamWithImagesAndContext processes a user message with images and separate display/storage versions.
// contextForLLM: the full message (with channel history) sent to LLM
// messageToStore: the clean message (without channel history markers) saved to conversation store
func (s *ChatService) HandleMessageStreamWithImagesAndContext(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string, images []domain.ImageInput) (<-chan llm.StreamChunk, error) {
	persona, err := s.resolvePersona(personaID)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Save images to disk and collect refs
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

	return s.streamWithMultimodalHistory(ctx, conv, persona, personaID, contextForLLM, messageToStore, images, imageRefs...)
}

func (s *ChatService) resolvePersona(personaID string) (*domain.PersonaModule, error) {
	persona, exists := s.personas[personaID]
	if !exists {
		return nil, fmt.Errorf("persona %q not found", personaID)
	}
	return persona, nil
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
func (s *ChatService) handleToolCallingStream(
	ctx context.Context,
	streamCh <-chan llm.StreamChunk,
	conv *domain.Conversation,
	messageToStore, contextForLLM string,
	persona *domain.PersonaModule,
	personaID string,
	rawLLMInput string,
	imageRefs []domain.ImageRef, // nil for text-only messages
) <-chan llm.StreamChunk {
	outputCh := make(chan llm.StreamChunk, 16)

	go func() {
		defer close(outputCh)

		var fullResponse string
		var pendingToolCalls []domain.ToolCall

		// Process initial stream
		for chunk := range streamCh {
			// Check for tool calls
			if len(chunk.ToolCalls) > 0 {
				pendingToolCalls = chunk.ToolCalls
				// Forward tool call indicator to frontend
				outputCh <- chunk
				continue
			}

			// Forward chunk to output
			outputCh <- chunk
			if chunk.Error != nil {
				return
			}
			fullResponse += chunk.Content
		}

		// Handle tool calls if present
		if len(pendingToolCalls) > 0 {
			// Log tool call names
			names := make([]string, len(pendingToolCalls))
			for i, c := range pendingToolCalls {
				names[i] = c.Name
			}
			logChat.Logf("tool calls: %s", strings.Join(names, ", "))

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
			outputCh <- llm.StreamChunk{
				ToolStatus: "tool_done",
			}

			// Build tool results message for LLM
			toolResultsText := formatToolResults(pendingToolCalls, toolResults)

			// Append assistant's tool call request to conversation
			conv.AppendAssistantMessage("[Tool calls executed]")

			// Append tool results as system message
			conv.Messages = append(conv.Messages, domain.Message{
				Role:    domain.RoleUser,
				Content: toolResultsText,
			})

			// Continue conversation with tool results
			logChat.Logf("continuing with tool results → LLM")
			assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("assemble context after tools: %w", err), Done: true}
				return
			}

			// Call LLM again with tool results
			continuationStream, err := s.provider.StreamChat(ctx, llm.ChatRequest{
				SystemPrompt: assembled.SystemPrompt,
				Messages:     assembled.Messages,
				Generation:   assembled.Generation,
			})
			if err != nil {
				outputCh <- llm.StreamChunk{Error: fmt.Errorf("llm continuation: %w", err), Done: true}
				return
			}

			// Forward continuation stream
			var finalResponse string
			for chunk := range continuationStream {
				outputCh <- chunk
				if chunk.Error != nil {
					return
				}
				finalResponse += chunk.Content
			}

			// Append tool usage annotation to final response (for frontend display)
			toolUsageAnnotation := formatToolUsageAnnotation(pendingToolCalls)
			finalResponseWithAnnotation := finalResponse + "\n\n" + toolUsageAnnotation

			// Stream the annotation as a separate chunk
			outputCh <- llm.StreamChunk{
				Content: "\n\n" + toolUsageAnnotation,
			}

			// Store final conversation with raw LLM data
			// For tool calls, the raw output includes the tool execution cycle
			rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s",
				toolResultsText, finalResponse)
			if len(imageRefs) > 0 {
				conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
			} else {
				conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			}
			conv.AppendAssistantMessageWithRaw(finalResponseWithAnnotation, rawLLMInput, rawLLMOutput)
			_ = s.store.Save(ctx, conv)

			// Fire background memory extraction (invisible to user)
			if s.memoryExtractor != nil {
				go s.memoryExtractor.Process(context.Background(), persona.DirName, messageToStore, finalResponse, persona)
			}

			outputCh <- llm.StreamChunk{Done: true}
			return
		}

		// No tool calls - store conversation normally with raw LLM data
		if fullResponse != "" || messageToStore != "" {
			if len(imageRefs) > 0 {
				conv.AppendUserMessageWithImages(messageToStore, contextForLLM, imageRefs)
			} else {
				conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			}
			conv.AppendAssistantMessageWithRaw(fullResponse, rawLLMInput, fullResponse)
			_ = s.store.Save(ctx, conv)

			// Fire background memory extraction (invisible to user)
			if s.memoryExtractor != nil {
				go s.memoryExtractor.Process(context.Background(), persona.DirName, messageToStore, fullResponse, persona)
			}
		}

		// Send final Done signal to indicate stream completion
		outputCh <- llm.StreamChunk{Done: true}
	}()

	return outputCh
}

// historyHasRecentImages checks if any of the last N user messages have image refs.
func (s *ChatService) historyHasRecentImages(conv *domain.Conversation) bool {
	userCount := 0
	for i := len(conv.Messages) - 1; i >= 0 && userCount < s.imageHistoryTurns; i-- {
		msg := conv.Messages[i]
		if msg.Role == domain.RoleUser {
			userCount++
			if len(msg.ImageRefs) > 0 {
				return true
			}
		}
	}
	return false
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
	currentImageRefs ...domain.ImageRef,
) (<-chan llm.StreamChunk, error) {
	// Build multimodal history from stored messages (load images from disk)
	multimodalHistory := s.buildMultimodalHistory(conv.Messages)

	// Build current user message parts
	userParts := []domain.MessagePart{{Text: contextForLLM}}
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

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

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

	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, personaID, rawLLMInput, currentImageRefs), nil
}

// buildMultimodalHistory converts stored messages to MultimodalMessage format,
// loading images from disk for the most recent N user turns.
func (s *ChatService) buildMultimodalHistory(messages []domain.Message) []domain.MultimodalMessage {
	// Find which user messages are within the image history window
	recentImageTurns := make(map[int]bool)
	userCount := 0
	for i := len(messages) - 1; i >= 0 && userCount < s.imageHistoryTurns; i-- {
		if messages[i].Role == domain.RoleUser {
			userCount++
			if len(messages[i].ImageRefs) > 0 {
				recentImageTurns[i] = true
			}
		}
	}

	result := make([]domain.MultimodalMessage, 0, len(messages))
	for i, msg := range messages {
		parts := []domain.MessagePart{{Text: msg.Content}}

		if recentImageTurns[i] && s.imageStore != nil {
			// Load images from disk for recent turns
			for _, ref := range msg.ImageRefs {
				img, err := s.imageStore.Load(ref)
				if err != nil {
					logChat.Warnf("load image %s: %v (substituting placeholder)", ref.StorageKey, err)
					parts = append(parts, domain.MessagePart{
						Text: fmt.Sprintf("[Image unavailable: %s]", ref.OriginalFilename),
					})
					continue
				}
				parts = append(parts, domain.MessagePart{ImageData: &img})
			}
		} else if len(msg.ImageRefs) > 0 {
			// Older turns: add text placeholders
			for _, ref := range msg.ImageRefs {
				parts = append(parts, domain.MessagePart{
					Text: fmt.Sprintf("[Previous image: %s]", ref.OriginalFilename),
				})
			}
		}

		result = append(result, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     parts,
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

		formatted += fmt.Sprintf("Tool: %s (ID: %s)\n", toolName, result.ToolCallID)
		if result.IsError {
			formatted += fmt.Sprintf("Status: ERROR\n%s\n\n", result.Content)
		} else {
			formatted += fmt.Sprintf("Status: SUCCESS\n%s\n\n", result.Content)
		}
	}
	return formatted
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
