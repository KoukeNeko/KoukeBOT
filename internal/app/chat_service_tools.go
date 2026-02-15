package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// executeTools executes a list of tool calls and returns the results.
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
