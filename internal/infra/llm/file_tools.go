package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxFileSize = 1 * 1024 * 1024 // 1MB 限制

// validatePath 驗證檔案路徑安全性
func validatePath(personaDir, filename string) error {
	if strings.Contains(filename, "..") {
		return fmt.Errorf("不允許路徑遍歷")
	}
	if strings.HasPrefix(filename, "/") {
		return fmt.Errorf("不允許絕對路徑")
	}

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

	if info, err := os.Stat(absTargetPath); err == nil {
		if info.Size() > maxFileSize {
			return fmt.Errorf("檔案過大（最大 %d bytes）", maxFileSize)
		}
	}

	return nil
}

func normalizeManagedMemoryPath(filename string) (string, string) {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return filename, ""
	}
	cleaned := filepath.Clean(trimmed)
	cleaned = strings.TrimPrefix(cleaned, "."+string(filepath.Separator))
	cleaned = filepath.ToSlash(cleaned)

	switch strings.ToLower(cleaned) {
	case "memory.md":
		return "memory/MEMORY.md", "ℹ️ 請改用 memory/MEMORY.md（不要使用根目錄 MEMORY.md）。"
	case "memory/memory.md":
		return "memory/MEMORY.md", "ℹ️ 請固定使用 memory/MEMORY.md。"
	default:
		return cleaned, ""
	}
}
