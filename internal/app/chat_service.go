package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// ChatService orchestrates the full chat pipeline:
// receive message → load conversation → assemble context → call LLM → persist → return.
type ChatService struct {
	provider     llm.Provider
	assembler    *ContextAssembler
	store        store.ConversationRepository
	personas     map[string]*domain.PersonaModule
	toolRegistry map[string]domain.Tool // tool name → tool instance
}

func NewChatService(
	provider llm.Provider,
	assembler *ContextAssembler,
	convStore store.ConversationRepository,
	personas map[string]*domain.PersonaModule,
	tools []domain.Tool,
) *ChatService {
	// Build tool registry
	toolRegistry := make(map[string]domain.Tool)
	for _, tool := range tools {
		toolRegistry[tool.Name()] = tool
	}

	return &ChatService{
		provider:     provider,
		assembler:    assembler,
		store:        convStore,
		personas:     personas,
		toolRegistry: toolRegistry,
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
	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, rawLLMInput), nil
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

	// Build multimodal message with LLM context (includes channel history) and images
	userParts := []domain.MessagePart{{Text: contextForLLM}}
	for _, img := range images {
		userParts = append(userParts, domain.MessagePart{ImageData: &img})
	}

	// Convert conversation history to multimodal format
	multimodalHistory := make([]domain.MultimodalMessage, 0, len(conv.Messages)+1)
	for _, msg := range conv.Messages {
		multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     []domain.MessagePart{{Text: msg.Content}},
			Timestamp: msg.Timestamp,
		})
	}
	// Append current user message with images
	multimodalHistory = append(multimodalHistory, domain.MultimodalMessage{
		Role:  domain.RoleUser,
		Parts: userParts,
	})

	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

	// Build raw LLM input for transparency (multimodal messages converted to text representation)
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

	// Handle tool calling loop and wrap stream for persistence
	return s.handleToolCallingStream(ctx, streamCh, conv, messageToStore, contextForLLM, persona, rawLLMInput), nil
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
		result, err := tool.Execute(ctx, call.Input)
		if err != nil {
			// Execution error - wrap in error result
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error executing tool: %v", err),
				IsError:    true,
			})
			continue
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
	rawLLMInput string,
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
			// Execute tools
			toolResults := s.executeTools(ctx, pendingToolCalls)

			// Build tool results message for LLM
			toolResultsText := formatToolResults(toolResults)

			// Append assistant's tool call request to conversation
			conv.AppendAssistantMessage("[Tool calls executed]")

			// Append tool results as system message
			conv.Messages = append(conv.Messages, domain.Message{
				Role:    domain.RoleUser,
				Content: toolResultsText,
			})

			// Continue conversation with tool results
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

			// Store final conversation with raw LLM data
			// For tool calls, the raw output includes the tool execution cycle
			rawLLMOutput := fmt.Sprintf("[Tool calls executed]\n%s\n\n[Final response]\n%s",
				toolResultsText, finalResponse)
			conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			conv.AppendAssistantMessageWithRaw(finalResponse, rawLLMInput, rawLLMOutput)
			_ = s.store.Save(ctx, conv)

			outputCh <- llm.StreamChunk{Done: true}
			return
		}

		// No tool calls - store conversation normally with raw LLM data
		if fullResponse != "" || messageToStore != "" {
			conv.AppendUserMessageWithDisplay(messageToStore, contextForLLM)
			conv.AppendAssistantMessageWithRaw(fullResponse, rawLLMInput, fullResponse)
			_ = s.store.Save(ctx, conv)
		}

		// Send final Done signal to indicate stream completion
		outputCh <- llm.StreamChunk{Done: true}
	}()

	return outputCh
}

// formatToolResults formats tool results for LLM consumption
func formatToolResults(results []domain.ToolResult) string {
	var formatted string
	formatted += "Tool execution results:\n\n"
	for _, result := range results {
		formatted += fmt.Sprintf("Tool Call ID: %s\n", result.ToolCallID)
		if result.IsError {
			formatted += fmt.Sprintf("Status: ERROR\n%s\n\n", result.Content)
		} else {
			formatted += fmt.Sprintf("Status: SUCCESS\n%s\n\n", result.Content)
		}
	}
	return formatted
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
