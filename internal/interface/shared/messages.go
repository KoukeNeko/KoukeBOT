package shared

import (
	"fmt"
	"strings"
)

type ErrorKind int

const (
	ErrorKindUnknown ErrorKind = iota
	ErrorKindQuota
	ErrorKindAuth
	ErrorKindNetwork
	ErrorKindRateLimit
)

type ParsedError struct {
	Kind       ErrorKind
	ResetAfter string
	RawMessage string
}

func ParseLLMError(err error) ParsedError {
	if err == nil {
		return ParsedError{}
	}
	errMsg := err.Error()
	out := ParsedError{RawMessage: errMsg, Kind: ErrorKindUnknown}

	if strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "QUOTA_EXHAUSTED") ||
		strings.Contains(errMsg, "exhausted your capacity") {
		out.Kind = ErrorKindQuota
		out.ResetAfter = extractResetAfter(errMsg)
		return out
	}
	if strings.Contains(errMsg, "401") ||
		strings.Contains(errMsg, "Unauthorized") ||
		strings.Contains(errMsg, "UNAUTHENTICATED") {
		out.Kind = ErrorKindAuth
		return out
	}
	if strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "network") {
		out.Kind = ErrorKindNetwork
		return out
	}
	if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "too many requests") {
		out.Kind = ErrorKindRateLimit
		return out
	}

	return out
}

func extractResetAfter(errMsg string) string {
	if !strings.Contains(errMsg, "quota will reset after") {
		return ""
	}
	start := strings.Index(errMsg, "reset after ")
	if start < 0 {
		return ""
	}
	start += len("reset after ")
	end := strings.Index(errMsg[start:], ".")
	if end <= 0 {
		return ""
	}
	return errMsg[start : start+end]
}

func FormatToolStartMessage(toolName string, toolInput map[string]interface{}) string {
	switch toolName {
	case "web_search":
		query := getString(toolInput, "query")
		if query != "" {
			return fmt.Sprintf("🔍 正在搜尋：%s", query)
		}
		return "🔍 正在搜尋..."
	case "web_fetch":
		url := getString(toolInput, "url")
		if url != "" {
			return fmt.Sprintf("📄 正在擷取網頁：%s", url)
		}
		return "📄 正在擷取網頁..."
	case "file_read":
		filename := getString(toolInput, "filename")
		if filename != "" {
			return fmt.Sprintf("📖 正在讀取檔案：%s", filename)
		}
		return "📖 正在讀取檔案..."
	case "file_write":
		filename := getString(toolInput, "filename")
		if filename != "" {
			return fmt.Sprintf("💾 正在寫入檔案：%s", filename)
		}
		return "💾 正在寫入檔案..."
	case "file_append":
		filename := getString(toolInput, "filename")
		if filename != "" {
			return fmt.Sprintf("📝 正在追加內容到：%s", filename)
		}
		return "📝 正在追加內容..."
	case "session_status":
		return "⏰ 正在取得時間資訊..."
	case "discord_observe":
		return "🛰️ 正在讀取 Discord 狀態..."
	default:
		if strings.TrimSpace(toolName) == "" {
			toolName = "unknown"
		}
		return fmt.Sprintf("🔧 正在使用工具：%s", toolName)
	}
}

func getString(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	val, ok := input[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(val)
}
