package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
	interfacecommon "github.com/KoukeNeko/KoukeBOT/internal/interface/shared"
)

// resolveSenderLabel returns a human-readable label for the message author.
func resolveSenderLabel(author *discordgo.User) string {
	if author == nil {
		return ""
	}

	displayName := strings.TrimSpace(author.GlobalName)
	if displayName == "" {
		displayName = strings.TrimSpace(author.Username)
	}
	if displayName == "" {
		return author.ID
	}

	discriminator := strings.TrimSpace(author.Discriminator)
	if discriminator != "" && discriminator != "0" {
		return fmt.Sprintf("%s#%s", displayName, discriminator)
	}

	return displayName
}

// formatInboundEnvelope formats an inbound Discord message into a TOON msg event.
func formatInboundEnvelope(channel, sender, userID, channelID, personaID string, timestamp time.Time, body, priority string) (string, error) {
	return tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    strings.ToLower(strings.TrimSpace(channel)),
		UserID:    strings.TrimSpace(userID),
		Name:      strings.TrimSpace(sender),
		ChannelID: strings.TrimSpace(channelID),
		PersonaID: strings.TrimSpace(personaID),
		Timestamp: timestamp.UTC(),
		Text:      body,
		Priority:  priority,
	})
}

// formatInboundEnvelopeLegacy preserves the previous human-readable envelope format.
func formatInboundEnvelopeLegacy(channel, sender, userID, timestamp, body string) string {
	envelope := fmt.Sprintf("[%s %s (id: %s) %s] %s", channel, sender, userID, timestamp, body)
	if sender != "" {
		envelope = envelope + fmt.Sprintf("\n[from: %s (id: %s)]", sender, userID)
	}
	return envelope
}

// formatToolCallIndicator formats tool call information for Discord display.
func formatToolCallIndicator(toolCalls []domain.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	var indicators []string
	for _, call := range toolCalls {
		switch call.Name {
		case "web_search":
			query := ""
			if q, ok := call.Input["query"].(string); ok {
				query = q
			}
			if query != "" {
				indicators = append(indicators, fmt.Sprintf("🔍 正在搜尋: %s", query))
			} else {
				indicators = append(indicators, "🔍 正在搜尋...")
			}
		default:
			indicators = append(indicators, fmt.Sprintf("🔧 使用工具: %s", call.Name))
		}
	}

	return strings.Join(indicators, "\n")
}

func formatToolStatusError(toolName string, toolInput map[string]interface{}, rawErr string) string {
	errText := strings.TrimSpace(rawErr)
	if errText == "" {
		errText = "未知錯誤"
	}
	lowerErr := strings.ToLower(errText)

	if strings.HasPrefix(lowerErr, "tool guard:") {
		return fmt.Sprintf("⚠️ [TOOL_GUARD] 工具保護機制觸發（%s）：%s", safeToolName(toolName), errText)
	}

	switch toolName {
	case "file_write":
		return fmt.Sprintf("❌ [TOOL_ERROR] 儲存檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	case "file_append":
		return fmt.Sprintf("❌ [TOOL_ERROR] 追加檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	case "file_read":
		return fmt.Sprintf("❌ [TOOL_ERROR] 讀取檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	default:
		if toolName == "" {
			toolName = "unknown"
		}
		return fmt.Sprintf("❌ [TOOL_ERROR] 工具 %s 執行失敗：%s", toolName, errText)
	}
}

func formatToolGuardStatusDetail(toolName string, rawErr string) string {
	errText := strings.TrimSpace(rawErr)
	if errText == "" {
		errText = "tool guard triggered without details"
	}
	return fmt.Sprintf("⚠️ [TOOL_GUARD_DETAIL] tool=%s detail=%s", safeToolName(toolName), errText)
}

func formatToolStartStatusMessage(toolName string, toolInput map[string]interface{}) string {
	return interfacecommon.FormatToolStartMessage(toolName, toolInput)
}

func safeToolName(toolName string) string {
	if strings.TrimSpace(toolName) == "" {
		return "unknown"
	}
	return toolName
}

func extractToolFilename(toolInput map[string]interface{}) string {
	if toolInput == nil {
		return "unknown"
	}
	if v, ok := toolInput["filename"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := toolInput["file"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return "unknown"
}

// formatDiscordError converts technical errors to user-friendly Discord messages.
func formatDiscordError(err error) string {
	parsed := interfacecommon.ParseLLMError(err)
	switch parsed.Kind {
	case interfacecommon.ErrorKindModelCapacity:
		return "🔄 **AI 模型目前繁忙**\n伺服器暫時無法處理請求，請稍後再試\n\n💡 此為模型端的容量限制，通常幾分鐘內會恢復"
	case interfacecommon.ErrorKindCircuitOpen:
		return "⏸️ **AI 服務暫時停止**\n為避免重複失敗，已暫停請求\n系統會自動偵測恢復狀態，請稍後再試"
	case interfacecommon.ErrorKindQueueFull:
		return "📋 **目前請求量過大**\n等候佇列已滿，請稍後再試"
	case interfacecommon.ErrorKindQueueTimeout:
		return "⏱️ **已自動重試但等待逾時**\n請求已排隊等待可用帳號，但在期限內仍未恢復\n\n💡 模型繁忙通常是暫時的，請稍後再試"
	case interfacecommon.ErrorKindQuota:
		if parsed.ResetAfter != "" {
			return fmt.Sprintf("⏳ **API 配額已用完**\n請等待 **%s** 後重試\n\n💡 提示：可至 Web UI Settings 切換到 API Key 模式", parsed.ResetAfter)
		}
		return "⏳ **API 配額已用完**\n請稍後再試\n\n💡 提示：請至 Web UI Settings 切換到 API Key 模式，或等待配額重置"
	case interfacecommon.ErrorKindAuth:
		return "🔐 **認證失敗**\n請至 Web UI Settings 重新設定 API Key 或 OAuth"
	case interfacecommon.ErrorKindNetwork:
		return "🌐 **網路連線失敗**\n請檢查網路連線或稍後再試"
	case interfacecommon.ErrorKindRateLimit:
		return "⚠️ **請求過於頻繁**\n請稍等片刻後再試"
	}

	return fmt.Sprintf("❌ **發生錯誤**\n%s\n\n💡 請查看後端 log 或至 Web UI 檢查設定", parsed.RawMessage)
}
