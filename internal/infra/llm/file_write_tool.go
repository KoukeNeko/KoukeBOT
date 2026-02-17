package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// FileWriteTool 寫入或覆蓋檔案
type FileWriteTool struct {
	personaDir string
}

func NewFileWriteTool(personaDir string) *FileWriteTool {
	return &FileWriteTool{personaDir: personaDir}
}

func (t *FileWriteTool) Name() string {
	return "file_write"
}

func (t *FileWriteTool) Description() string {
	return "Write or overwrite a file in the persona directory. Use this to update user.md or save notes. WARNING: This overwrites the entire file."
}

func (t *FileWriteTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Filename to write (e.g., 'user.md', 'notes.txt')",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Full content to write to the file",
			},
		},
		"required": []string{"filename", "content"},
	}
}

func (t *FileWriteTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
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

	content, ok := input["content"].(string)
	if !ok {
		return domain.ToolResult{
			Content: "錯誤：必須提供檔案內容",
			IsError: true,
		}, nil
	}

	if err := validatePath(t.personaDir, filename); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("錯誤：%v", err),
			IsError: true,
		}, nil
	}

	targetPath := filepath.Join(t.personaDir, filename)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("建立目錄失敗：%v", err),
			IsError: true,
		}, nil
	}
	if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("寫入檔案失敗：%v", err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("✅ 成功寫入 %d 個字元到 %s", len(content), filename)
	if pathNotice != "" {
		result = result + "\n" + pathNotice
	}

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}
