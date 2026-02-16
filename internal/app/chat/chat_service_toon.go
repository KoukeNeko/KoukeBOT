package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func (s *ChatService) encodeUserMessageForLLM(ctx context.Context, channelID, personaID, text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}

	reqCtx, _ := llm.GetRequestContext(ctx)
	channel := strings.TrimSpace(reqCtx.ChannelID)
	if channel == "" {
		channel = strings.TrimSpace(channelID)
	}
	persona := strings.TrimSpace(reqCtx.PersonaID)
	if persona == "" {
		persona = strings.TrimSpace(personaID)
	}

	encoded, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    requestSourceForTOON(reqCtx.Source),
		UserID:    strings.TrimSpace(reqCtx.UserID),
		ChannelID: channel,
		PersonaID: persona,
		Timestamp: time.Now().UTC(),
		Text:      text,
		Priority:  tooncodec.PriorityNormal,
	})
	if err != nil {
		logChat.Warnf("toon message encode failed, fallback to legacy text: %v", err)
		return text
	}
	return encoded
}

func (s *ChatService) encodeUserMessageForStorage(ctx context.Context, channelID, personaID, text, fallback string) string {
	if encoded := s.encodeUserMessageForLLM(ctx, channelID, personaID, text); isTOONMessageEvent(encoded) {
		return encoded
	}

	fallback = strings.TrimSpace(fallback)
	if isTOONMessageEvent(fallback) {
		return fallback
	}
	if fallback != "" {
		humanized := strings.TrimSpace(tooncodec.HumanizeTOON(fallback))
		if humanized != "" {
			if encoded := s.encodeUserMessageForLLM(ctx, channelID, personaID, humanized); isTOONMessageEvent(encoded) {
				return encoded
			}
		}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		logChat.Warnf("toon user storage encode fallback to plain text")
		return text
	}
	return fallback
}

func (s *ChatService) encodeAssistantMessageForStorage(
	ctx context.Context,
	channelID string,
	persona *domain.PersonaModule,
	personaID string,
	text string,
) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}

	reqCtx, _ := llm.GetRequestContext(ctx)
	channel := strings.TrimSpace(reqCtx.ChannelID)
	if channel == "" {
		channel = strings.TrimSpace(channelID)
	}

	resolvedPersonaID := strings.TrimSpace(reqCtx.PersonaID)
	if resolvedPersonaID == "" {
		resolvedPersonaID = strings.TrimSpace(personaID)
	}
	if persona != nil {
		if resolvedPersonaID == "" {
			resolvedPersonaID = strings.TrimSpace(persona.Config.Meta.ID)
		}
	}
	if resolvedPersonaID == "" {
		resolvedPersonaID = "unknown"
	}

	assistantName := resolvedPersonaID
	if persona != nil {
		if name := strings.TrimSpace(persona.Config.Meta.Name); name != "" {
			assistantName = name
		}
	}

	encoded, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    requestSourceForTOON(reqCtx.Source),
		UserID:    fmt.Sprintf("bot:%s", resolvedPersonaID),
		Name:      assistantName,
		ChannelID: channel,
		PersonaID: resolvedPersonaID,
		Timestamp: time.Now().UTC(),
		Text:      text,
		Priority:  tooncodec.PriorityNormal,
	})
	if err != nil {
		logChat.Warnf("toon assistant message encode failed, fallback to plain text: %v", err)
		return text
	}
	return encoded
}

func (s *ChatService) ensureTOONContextForLLM(ctx context.Context, channelID, personaID, contextForLLM, messageToStore string) string {
	if eventType, ok := tooncodec.DecodeEventType(contextForLLM); ok {
		if eventType == tooncodec.EventTypeMessage || eventType == tooncodec.EventTypeContext {
			return contextForLLM
		}
	}

	targetText := strings.TrimSpace(messageToStore)
	if targetText == "" {
		targetText = strings.TrimSpace(contextForLLM)
	}
	if targetText == "" {
		return contextForLLM
	}
	return s.encodeUserMessageForLLM(ctx, channelID, personaID, targetText)
}

func requestSourceForTOON(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case llm.RequestSourceDiscord:
		return llm.RequestSourceDiscord
	case llm.RequestSourceWeb:
		return llm.RequestSourceWeb
	case llm.RequestSourceAPI:
		return llm.RequestSourceAPI
	default:
		return llm.RequestSourceAPI
	}
}

func isTOONMessageEvent(content string) bool {
	eventType, ok := tooncodec.DecodeEventType(strings.TrimSpace(content))
	return ok && eventType == tooncodec.EventTypeMessage
}
