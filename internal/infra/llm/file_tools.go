package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const maxFileSize = 1 * 1024 * 1024 // 1MB 限制

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
	// Support both "filename" and "file" parameter names for compatibility
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

	// 驗證路徑安全性
	if err := validatePath(t.personaDir, filename); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("錯誤：%v", err),
			IsError: true,
		}, nil
	}

	// 讀取檔案
	targetPath := filepath.Join(t.personaDir, filename)
	content, err := os.ReadFile(targetPath)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("讀取檔案失敗：%v", err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("=== 檔案: %s ===\n\n%s\n\n=== 檔案結束 ===", filename, string(content))

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}

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
	// Support both "filename" and "file" parameter names for compatibility
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

	content, ok := input["content"].(string)
	if !ok {
		return domain.ToolResult{
			Content: "錯誤：必須提供檔案內容",
			IsError: true,
		}, nil
	}

	// 驗證路徑安全性
	if err := validatePath(t.personaDir, filename); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("錯誤：%v", err),
			IsError: true,
		}, nil
	}

	// 寫入檔案
	targetPath := filepath.Join(t.personaDir, filename)
	if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("寫入檔案失敗：%v", err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("✅ 成功寫入 %d 個字元到 %s", len(content), filename)

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}

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
	// Support both "filename" and "file" parameter names for compatibility
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

	content, ok := input["content"].(string)
	if !ok {
		return domain.ToolResult{
			Content: "錯誤：必須提供檔案內容",
			IsError: true,
		}, nil
	}

	// 驗證路徑安全性
	if err := validatePath(t.personaDir, filename); err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("錯誤：%v", err),
			IsError: true,
		}, nil
	}

	// 追加內容
	targetPath := filepath.Join(t.personaDir, filename)
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

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}

// validatePath 驗證檔案路徑安全性
func validatePath(personaDir, filename string) error {
	// 防止目錄遍歷
	if strings.Contains(filename, "..") {
		return fmt.Errorf("不允許路徑遍歷")
	}
	if strings.HasPrefix(filename, "/") {
		return fmt.Errorf("不允許絕對路徑")
	}

	// 只允許特定副檔名
	ext := filepath.Ext(filename)
	allowedExts := []string{".md", ".txt", ".json"}
	allowed := false
	for _, allowedExt := range allowedExts {
		if ext == allowedExt {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("只允許 .md, .txt 和 .json 檔案")
	}

	// 驗證最終路徑在 persona 目錄內
	absPersonaDir, err := filepath.Abs(personaDir)
	if err != nil {
		return fmt.Errorf("解析 persona 目錄失敗：%w", err)
	}

	targetPath := filepath.Join(absPersonaDir, filename)
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("解析目標路徑失敗：%w", err)
	}

	if !strings.HasPrefix(absTargetPath, absPersonaDir) {
		return fmt.Errorf("路徑在 persona 目錄之外")
	}

	// 檢查檔案大小限制（如果檔案存在）
	if info, err := os.Stat(absTargetPath); err == nil {
		if info.Size() > maxFileSize {
			return fmt.Errorf("檔案過大（最大 %d bytes）", maxFileSize)
		}
	}

	return nil
}
