package discord

import (
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

// buildReplyReferenceEntry extracts the replied-to message as high-priority context.
func (h *Handler) buildReplyReferenceEntry(s *discordgo.Session, m *discordgo.MessageCreate, personaID string, cutoff *time.Time) *domain.ChannelHistoryEntry {
	if m == nil || m.MessageReference == nil || m.MessageReference.MessageID == "" {
		return nil
	}

	refMsg := m.ReferencedMessage
	if refMsg == nil && s != nil {
		refChannelID := m.ChannelID
		if m.MessageReference.ChannelID != "" {
			refChannelID = m.MessageReference.ChannelID
		}
		fetched, err := s.ChannelMessage(refChannelID, m.MessageReference.MessageID)
		if err != nil {
			logHandler.Warnf("reply-target fetch failed [%s/%s]: %v", refChannelID, m.MessageReference.MessageID, err)
			return nil
		}
		refMsg = fetched
	}
	if refMsg == nil {
		return nil
	}
	if !shouldIncludeDiscordTimestamp(refMsg.Timestamp, cutoff) {
		return nil
	}

	content := strings.TrimSpace(refMsg.Content)
	if content == "" && len(refMsg.Attachments) > 0 {
		content = buildAttachmentPlaceholder(refMsg.Attachments)
	}
	if content == "" {
		return nil
	}
	if len(content) > 350 {
		content = content[:350] + "..."
	}

	senderLabel := resolveSenderLabel(refMsg.Author)
	userID := ""
	if refMsg.Author != nil {
		userID = refMsg.Author.ID
	}
	formatted, err := formatInboundEnvelope(
		"Discord",
		senderLabel,
		userID,
		m.ChannelID,
		personaID,
		refMsg.Timestamp,
		content,
		tooncodec.PriorityReplyHigh,
	)
	if err != nil {
		logHandler.Warnf("toon reply-target encode failed, fallback to legacy envelope: %v", err)
		timestamp := refMsg.Timestamp.Format("2006-01-02 15:04 MST")
		formatted = "[Replied message - high priority]\n" +
			formatInboundEnvelopeLegacy("Discord", senderLabel, userID, timestamp, content)
	}

	var images []domain.ImageInput
	for _, att := range refMsg.Attachments {
		if len(images) >= 2 {
			break
		}
		if !isImageAttachment(att) {
			continue
		}
		img, err := downloadImage(att.URL)
		if err != nil {
			logHandler.Warnf("reply-target image download failed: %s: %v", att.Filename, err)
			continue
		}
		images = append(images, img)
	}

	return &domain.ChannelHistoryEntry{FormattedText: formatted, Images: images}
}

// buildChannelHistoryEntries builds structured channel history entries with image association.
func (h *Handler) buildChannelHistoryEntries(s *discordgo.Session, channelID, personaID, currentMessageID string, cutoff *time.Time) []domain.ChannelHistoryEntry {

	messages, err := s.ChannelMessages(channelID, defaultGroupHistoryLimit, currentMessageID, "", "")
	if err != nil {
		logHandler.Warnf("failed to fetch channel history [%s]: %v", channelID, err)
		return nil
	}
	if len(messages) == 0 {
		return nil
	}

	var entries []domain.ChannelHistoryEntry
	imageCount := 0
	usedChars := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !shouldIncludeDiscordTimestamp(msg.Timestamp, cutoff) {
			continue
		}
		content := msg.Content

		if strings.TrimSpace(content) == "" && len(msg.Attachments) > 0 {
			content = buildAttachmentPlaceholder(msg.Attachments)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}

		recentOffset := historyRecentOffset(len(messages), i)
		var entryImages []domain.ImageInput
		imageAgeOK := time.Since(msg.Timestamp.UTC()) < historyImageMaxAge
		if recentOffset < maxHistoryImageScan && imageCount < maxHistoryImages && imageAgeOK {
			for _, att := range msg.Attachments {
				if imageCount >= maxHistoryImages {
					break
				}
				if isImageAttachment(att) {
					img, err := downloadImage(att.URL)
					if err != nil {
						logHandler.Warnf("channel-history image download failed: %s: %v", att.Filename, err)
						continue
					}
					entryImages = append(entryImages, img)
					imageCount++
					senderLabel := ""
					if msg.Author != nil {
						senderLabel = resolveSenderLabel(msg.Author)
					}
					logHandler.Logf("channel-history image: %s from %s (%.1fKB)",
						att.Filename, senderLabel, float64(len(img.Data))/1024)
				}
			}
		}

		if len(content) > 200 {
			content = content[:200] + "..."
		}
		remaining := historyCharBudget - usedChars
		hasImages := len(entryImages) > 0
		if remaining <= 0 && !hasImages && len(entries) >= defaultHistoryTarget {
			continue
		}
		if remaining > 0 && len(content) > remaining && !hasImages && len(entries) >= 6 {
			content = content[:remaining] + "..."
		}

		senderLabel := resolveSenderLabel(msg.Author)
		userID := ""
		if msg.Author != nil {
			userID = msg.Author.ID
		}
		formattedMsg, formatErr := formatInboundEnvelope(
			"Discord",
			senderLabel,
			userID,
			channelID,
			personaID,
			msg.Timestamp,
			content,
			"",
		)
		if formatErr != nil {
			logHandler.Warnf("toon history message encode failed, fallback to legacy envelope: %v", formatErr)
			timestamp := msg.Timestamp.Format("2006-01-02 15:04 MST")
			formattedMsg = formatInboundEnvelopeLegacy("Discord", senderLabel, userID, timestamp, content)
		}

		entries = append(entries, domain.ChannelHistoryEntry{FormattedText: formattedMsg, Images: entryImages})
		usedChars += len(formattedMsg)

		if len(entries) >= defaultHistoryTarget && usedChars >= historyCharBudget {
			break
		}
	}

	return entries
}

func shouldIncludeDiscordTimestamp(ts time.Time, cutoff *time.Time) bool {
	if cutoff == nil {
		return true
	}
	return ts.After(*cutoff)
}

func historyRecentOffset(totalMessages, messageIndex int) int {
	return totalMessages - 1 - messageIndex
}

// buildContextForLLM rebuilds flat context string from structured history entries.
func buildContextForLLM(entries []domain.ChannelHistoryEntry, currentMessage, channelID, personaID string) string {
	if len(entries) == 0 {
		return currentMessage
	}

	historyEvents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.FormattedText) == "" {
			continue
		}
		historyEvents = append(historyEvents, entry.FormattedText)
	}
	encoded, err := tooncodec.EncodeContextEvent(tooncodec.ContextEventInput{
		Source:    llm.RequestSourceDiscord,
		ChannelID: channelID,
		PersonaID: personaID,
		History:   historyEvents,
		Current:   currentMessage,
	})
	if err == nil {
		return encoded
	}

	logHandler.Warnf("toon context encode failed, fallback to legacy context: %v", err)
	var result strings.Builder
	result.WriteString(domain.HistoryContextMarker)
	result.WriteString("\n")
	for i, entry := range entries {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(entry.FormattedText)
	}
	result.WriteString("\n\n")
	result.WriteString(domain.CurrentMessageMarker)
	result.WriteString("\n")
	result.WriteString(currentMessage)

	return result.String()
}
