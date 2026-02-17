package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// FileAppendTool 追加內容到檔案末尾
type FileAppendTool struct {
	personaDir string
}

func NewFileAppendTool(personaDir string) *FileAppendTool {
	return &FileAppendTool{personaDir: personaDir}
}

func (t *FileAppendTool) Name() string {
	return "file_append"
}

func (t *FileAppendTool) Description() string {
	return "Append content to the end of a file. Use this to add notes without overwriting existing content."
}

func (t *FileAppendTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Filename to append to",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to append",
			},
		},
		"required": []string{"filename", "content"},
	}
}

func (t *FileAppendTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
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
	f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("開啟檔案失敗：%v", err),
			IsError: true,
		}, nil
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("追加內容失敗：%v", err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("✅ 成功追加 %d 個字元到 %s", len(content), filename)
	if pathNotice != "" {
		result = result + "\n" + pathNotice
	}

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}
