package discord

import (
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// streamToDiscord collects streaming chunks and updates the Discord message in real-time.
func (h *Handler) streamToDiscord(channelID, replyToMessageID string, streamCh <-chan llm.StreamChunk) {
	presenter := newDiscordStreamPresenter(h, channelID, replyToMessageID)
	for chunk := range streamCh {
		done, stop := presenter.processChunk(chunk)
		if stop {
			return
		}
		if done {
			break
		}
	}
	presenter.sendFinal()
}

// splitMessage breaks a long message into chunks that fit Discord's limit.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var segments []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			segments = append(segments, text)
			break
		}

		cutPoint := strings.LastIndex(text[:maxLen], "\n")
		if cutPoint <= 0 {
			cutPoint = maxLen
		}

		segments = append(segments, text[:cutPoint])
		text = text[cutPoint:]
		text = strings.TrimPrefix(text, "\n")
	}

	return segments
}
