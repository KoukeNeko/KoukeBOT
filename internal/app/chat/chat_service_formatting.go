package chat

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

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

// formatToolResults formats tool results for LLM consumption.
func formatToolResults(toolCalls []domain.ToolCall, results []domain.ToolResult) string {
	// Create a map for quick lookup of tool calls by ID
	callMap := make(map[string]domain.ToolCall)
	for _, call := range toolCalls {
		callMap[call.ID] = call
	}

	items := make([]tooncodec.ToolResultInput, 0, len(results))
	for _, result := range results {
		call, exists := callMap[result.ToolCallID]
		toolName := "unknown"
		if exists {
			toolName = call.Name
		}

		compressed := compressToolResult(toolName, result.Content)
		items = append(items, tooncodec.ToolResultInput{
			ID:   result.ToolCallID,
			Name: toolName,
			OK:   !result.IsError,
			Out:  compressed,
		})
	}

	encoded, err := tooncodec.EncodeToolResultsEvent(items)
	if err != nil {
		logChat.Warnf("toon tool result encode failed, fallback to legacy text: %v", err)
		return formatToolResultsLegacy(toolCalls, results)
	}
	return encoded
}

func formatToolResultsLegacy(toolCalls []domain.ToolCall, results []domain.ToolResult) string {
	var formatted string
	formatted += "Tool execution results:\n\n"

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

// formatToolUsageAnnotation creates a user-friendly annotation for tool usage.
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
		case "schedule_manager":
			toolEmoji = "⏱️"
			toolDisplayName = "排程管理"
			if action, ok := call.Input["action"].(string); ok && strings.TrimSpace(action) != "" {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**：`%s`\n", i+1, toolEmoji, toolDisplayName, action))
			} else {
				annotation.WriteString(fmt.Sprintf("%d. %s **%s**\n", i+1, toolEmoji, toolDisplayName))
			}
		default:
			annotation.WriteString(fmt.Sprintf("%d. %s **%s**\n", i+1, toolEmoji, toolDisplayName))
		}
	}

	return annotation.String()
}

// formatLLMPrompt builds a complete LLM prompt string for debugging/transparency.
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

// formatLLMPromptHuman builds a humanized prompt string for debugging/transparency.
func formatLLMPromptHuman(systemPrompt string, messages []domain.Message) string {
	var prompt strings.Builder

	prompt.WriteString("=== SYSTEM PROMPT ===\n")
	prompt.WriteString(systemPrompt)
	prompt.WriteString("\n\n=== CONVERSATION HISTORY ===\n")

	for i, msg := range messages {
		prompt.WriteString(fmt.Sprintf("\n[Message %d - %s]\n", i+1, msg.Role))
		prompt.WriteString(tooncodec.HumanizeTOON(msg.Content))
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
		reason := strings.TrimSpace(view.Reason)
		if reason == "" {
			reason = "unknown"
		}
		logChat.Warnf("prompt_guard.triplet_parse_failed reason=%s salvaged=%t", reason, view.Salvaged)
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

// truncateLog truncates a string for log output.
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
