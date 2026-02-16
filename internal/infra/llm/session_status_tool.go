package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/util"
)

// SessionStatusTool 提供當前會話狀態（時間、日期等）
type SessionStatusTool struct {
	timezone   string
	timeFormat string // "12" | "24" | "auto"
}

// NewSessionStatusTool 建立新的 session_status 工具實例
func NewSessionStatusTool(timezone, timeFormat string) *SessionStatusTool {
	if timezone == "" || timezone == "auto" {
		timezone = "Local"
	}
	if timeFormat == "" {
		timeFormat = "auto"
	}
	return &SessionStatusTool{
		timezone:   timezone,
		timeFormat: timeFormat,
	}
}

// Name 返回工具識別名稱
func (t *SessionStatusTool) Name() string {
	return "session_status"
}

// Description 返回工具描述（給 LLM 看）
func (t *SessionStatusTool) Description() string {
	return "Get current session status including the current date, time, and day of week. Use this when you need to know the exact current time or date."
}

// InputSchema 返回工具參數的 JSON Schema
func (t *SessionStatusTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

// Execute 執行工具並返回當前時間狀態
func (t *SessionStatusTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	now := time.Now()

	// 決定是否使用 24 小時制
	use24h := t.timeFormat == "24"
	if t.timeFormat == "auto" {
		// 簡化版：亞洲時區預設用 24 小時制
		use24h = true
	}

	// 格式化時間（OpenClaw 風格）
	formattedTime := util.FormatUserTime(now, t.timezone, use24h)

	// 建立狀態報告
	status := map[string]interface{}{
		"current_time":   formattedTime,
		"timezone":       t.timezone,
		"unix_timestamp": now.Unix(),
	}

	statusJSON, _ := json.MarshalIndent(status, "", "  ")

	result := fmt.Sprintf(
		"🕒 Current Session Status\n\n%s\n\nNote: This is the current time when you called this tool.\n"+
			"Instruction: Treat `current_time`/`timezone` as authoritative for this turn; do NOT recompute timezone from `unix_timestamp` unless the user explicitly asks.\n"+
			"Instruction: Use this result directly and do NOT call `session_status` again in this turn unless the user explicitly asks for refreshed current time.",
		string(statusJSON),
	)

	return domain.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}
