package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// FileReadTool 讀取 persona 目錄內的檔案
type FileReadTool struct {
	personaDir string
}

func NewFileReadTool(personaDir string) *FileReadTool {
	return &FileReadTool{personaDir: personaDir}
}

func (t *FileReadTool) Name() string {
	return "file_read"
}

func (t *FileReadTool) Description() string {
	return "Read a file from the persona directory (e.g., 'user.md', 'notes.txt'). Use this to access user information or your own notes."
}

func (t *FileReadTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Filename to read (relative to persona directory). Example: 'user.md', 'notes.txt'",
			},
		},
		"required": []string{"filename"},
	}
}

func (t *FileReadTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	filename, ok := input["filename"].(string)
	if !ok || filename == "" {
		filename, ok = input["file"].(string)
	}
	if !ok || filename == "" {
		return domain.ToolResult{
			Content: "錯誤：必須提供檔案名稱（filename 或 file 參數）",
			IsError: true,
		}, nil
	}
	filename, pathNotice := normalizeManagedMemoryPath(filename)

	if err := validatePath(t.personaDir, filename); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("錯誤：%v", err),
			IsError: true,
		}, nil
	}

	targetPath := filepath.Join(t.personaDir, filename)
	content, err := os.ReadFile(targetPath)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("讀取檔案失敗：%v", err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("=== 檔案: %s ===\n\n%s\n\n=== 檔案結束 ===", filename, string(content))
	if pathNotice != "" {
		result = pathNotice + "\n" + result
	}

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}
