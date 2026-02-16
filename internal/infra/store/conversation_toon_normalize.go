package store

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

var (
	toonNormalizedConvertedCount uint64
	toonNormalizedFailedCount    uint64
)

func hydrateConversation(conv *domain.Conversation, now time.Time) {
	if conv == nil {
		return
	}
	if conv.ID == "" {
		conv.ID = NewConversationID()
	}
	if conv.Messages == nil {
		conv.Messages = make([]domain.Message, 0)
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	if conv.UpdatedAt.IsZero() {
		conv.UpdatedAt = now
	}
	normalizeConversationMessagesToTOON(conv, now)
}

func normalizeConversationMessagesToTOON(conv *domain.Conversation, now time.Time) {
	if conv == nil {
		return
	}

	converted := 0
	failed := 0

	for i := range conv.Messages {
		msg := &conv.Messages[i]
		if msg.Timestamp.IsZero() {
			msg.Timestamp = now
		}
		// Prevent system prompt snapshots from persisting in conversation history.
		msg.RawLLMInput = ""
		msg.RawLLMInputHuman = ""

		if strings.TrimSpace(msg.DisplayText) == "" {
			msg.DisplayText = humanizeMessageContent(msg.Content)
		}

		if msg.Role != domain.RoleUser && msg.Role != domain.RoleAssistant {
			continue
		}
		if isTOONMessageContent(msg.Content) {
			if strings.TrimSpace(msg.DisplayText) == "" {
				msg.DisplayText = humanizeMessageContent(msg.Content)
			}
			continue
		}

		text := textForTOONMessage(msg)
		if strings.TrimSpace(text) == "" {
			failed++
			continue
		}

		personaID := strings.TrimSpace(conv.PersonaID)
		userID := ""
		name := ""
		if msg.Role == domain.RoleAssistant {
			if personaID == "" {
				personaID = "unknown"
			}
			userID = "bot:" + personaID
			name = personaID
		}

		encoded, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
			Source:    inferTOONSource(msg.Content),
			UserID:    userID,
			Name:      name,
			ChannelID: strings.TrimSpace(conv.ChannelID),
			PersonaID: personaID,
			Timestamp: msg.Timestamp.UTC(),
			Text:      text,
			Priority:  tooncodec.PriorityNormal,
		})
		if err != nil {
			logStore.Warnf("toon normalize encode failed: role=%s err=%v", msg.Role, err)
			failed++
			continue
		}

		msg.Content = encoded
		if strings.TrimSpace(msg.DisplayText) == "" {
			msg.DisplayText = strings.TrimSpace(text)
		}
		converted++
	}

	if converted > 0 {
		total := atomic.AddUint64(&toonNormalizedConvertedCount, uint64(converted))
		logStore.Logf("metric toon.normalize.converted=%d (+%d)", total, converted)
	}
	if failed > 0 {
		total := atomic.AddUint64(&toonNormalizedFailedCount, uint64(failed))
		logStore.Warnf("metric toon.normalize.failed=%d (+%d)", total, failed)
	}
}

func isTOONMessageContent(content string) bool {
	eventType, ok := tooncodec.DecodeEventType(strings.TrimSpace(content))
	return ok && eventType == tooncodec.EventTypeMessage
}

func humanizeMessageContent(content string) string {
	if text, ok := tooncodec.ExtractMessageText(content); ok {
		return strings.TrimSpace(text)
	}
	humanized := strings.TrimSpace(tooncodec.HumanizeTOON(content))
	if humanized != "" {
		return humanized
	}
	return strings.TrimSpace(content)
}

func textForTOONMessage(msg *domain.Message) string {
	if msg == nil {
		return ""
	}

	trimmed := strings.TrimSpace(msg.Content)
	if trimmed == "" {
		return strings.TrimSpace(msg.DisplayText)
	}

	if eventType, ok := tooncodec.DecodeEventType(trimmed); ok {
		if eventType == tooncodec.EventTypeMessage {
			return humanizeMessageContent(trimmed)
		}
		if text := strings.TrimSpace(msg.DisplayText); text != "" {
			return text
		}
		return humanizeMessageContent(trimmed)
	}

	return msg.Content
}

func inferTOONSource(content string) string {
	lower := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.Contains(lower, "discord"):
		return "discord"
	case strings.Contains(lower, "web"):
		return "web"
	default:
		return "api"
	}
}
