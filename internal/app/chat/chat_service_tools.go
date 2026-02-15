package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// executeTools executes a list of tool calls and returns the results.
func (s *ChatService) executeTools(ctx context.Context, toolCalls []domain.ToolCall, personaID string) []domain.ToolResult {
	results := make([]domain.ToolResult, 0, len(toolCalls))
	if len(toolCalls) == 0 {
		return results
	}

	logChat.Logf("tool batch start: total=%d persona=%s", len(toolCalls), strings.TrimSpace(personaID))
	successCount := 0
	errorCount := 0

	for idx, call := range toolCalls {
		targetSummary := summarizeToolTarget(call, personaID)
		callID := summarizeToolCallID(call.ID)
		inputSummary := summarizeToolInput(call.Input)
		logChat.Logf(
			"tool[%d/%d] start: id=%s name=%s%s input=%s",
			idx+1,
			len(toolCalls),
			callID,
			call.Name,
			targetSummary,
			inputSummary,
		)

		tool, err := s.resolveTool(call.Name, personaID)
		if err != nil {
			errorCount++
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    err.Error(),
				IsError:    true,
			})
			logChat.Warnf(
				"tool[%d/%d] resolve failed: id=%s name=%s%s err=%s",
				idx+1,
				len(toolCalls),
				callID,
				call.Name,
				targetSummary,
				truncateLog(err.Error(), 180),
			)
			continue
		}

		// Execute tool
		startedAt := time.Now()
		result, err := tool.Execute(ctx, call.Input)
		elapsed := time.Since(startedAt)
		if err != nil {
			errorCount++
			logChat.Warnf(
				"tool[%d/%d] execute error: id=%s name=%s%s elapsed=%s err=%s",
				idx+1,
				len(toolCalls),
				callID,
				call.Name,
				targetSummary,
				formatToolElapsed(elapsed),
				truncateLog(err.Error(), 180),
			)
			results = append(results, domain.ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("Error executing tool: %v", err),
				IsError:    true,
			})
			continue
		}

		if result.IsError {
			errorCount++
			logChat.Warnf(
				"tool[%d/%d] done: id=%s name=%s%s status=error elapsed=%s output_chars=%d output=%s",
				idx+1,
				len(toolCalls),
				callID,
				call.Name,
				targetSummary,
				formatToolElapsed(elapsed),
				len(result.Content),
				summarizeToolOutput(result.Content),
			)
		} else {
			successCount++
			logChat.Logf(
				"tool[%d/%d] done: id=%s name=%s%s status=ok elapsed=%s output_chars=%d output=%s",
				idx+1,
				len(toolCalls),
				callID,
				call.Name,
				targetSummary,
				formatToolElapsed(elapsed),
				len(result.Content),
				summarizeToolOutput(result.Content),
			)
		}

		// Success - set tool call ID
		result.ToolCallID = call.ID
		results = append(results, result)
	}

	logChat.Logf("tool batch done: total=%d success=%d error=%d", len(toolCalls), successCount, errorCount)

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

	s.toolRegistryMu.RLock()
	tool, exists := s.toolRegistry[name]
	s.toolRegistryMu.RUnlock()
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

func summarizeToolCallID(id string) string {
	if strings.TrimSpace(id) == "" {
		return "n/a"
	}
	return truncateLog(id, 48)
}

func summarizeToolInput(input map[string]interface{}) string {
	if len(input) == 0 {
		return "{}"
	}
	b, err := json.Marshal(input)
	if err != nil {
		return truncateLog(fmt.Sprintf("%v", input), 220)
	}
	return truncateLog(singleLineToolLog(string(b)), 220)
}

func summarizeToolOutput(content string) string {
	if strings.TrimSpace(content) == "" {
		return `""`
	}
	return truncateLog(singleLineToolLog(content), 220)
}

func formatToolElapsed(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}

func singleLineToolLog(text string) string {
	line := strings.ReplaceAll(text, "\n", `\n`)
	line = strings.ReplaceAll(line, "\r", "")
	return line
}
