package discord

import (
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
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
	ts := formatHistoryTimestamp(h.inTimezone(refMsg.Timestamp))

	isBot := isBotAuthor(s, refMsg.Author)
	var formatted string
	if isBot {
		formatted = content
	} else {
		formatted = "[↩ " + senderLabel + " · " + ts + "] " + content
	}

	var images []domain.ImageInput
	for _, att := range refMsg.Attachments {
		if len(images) >= maxReplyReferenceImages {
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

	return &domain.ChannelHistoryEntry{FormattedText: formatted, Images: images, IsBot: isBot}
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

		isBot := isBotAuthor(s, msg.Author)
		senderLabel := resolveSenderLabel(msg.Author)
		ts := formatHistoryTimestamp(h.inTimezone(msg.Timestamp))

		var formattedMsg string
		if isBot {
			formattedMsg = content
		} else {
			formattedMsg = "[" + senderLabel + " · " + ts + "] " + content
		}

		entries = append(entries, domain.ChannelHistoryEntry{
			FormattedText: formattedMsg,
			Images:        entryImages,
			IsBot:         isBot,
		})
		usedChars += len(formattedMsg)

		if len(entries) >= defaultHistoryTarget && usedChars >= historyCharBudget {
			break
		}
	}

	return entries
}

// buildEphemeralHistory converts channel history entries into native role-based messages.
// Each entry with IsBot=true becomes an assistant message; others become user messages.
// The returned slice is intended to be temporarily prepended to the conversation
// before LLM assembly and must not be persisted.
func buildEphemeralHistory(entries []domain.ChannelHistoryEntry) []domain.Message {
	messages := make([]domain.Message, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.FormattedText) == "" {
			continue
		}
		role := domain.RoleUser
		if entry.IsBot {
			role = domain.RoleAssistant
		}
		messages = append(messages, domain.Message{Role: role, Content: entry.FormattedText})
	}
	return messages
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

// formatHistoryTimestamp returns a fixed "MM/DD HH:mm" timestamp for channel
// history labels, ensuring consistent temporal context regardless of day boundary.
func formatHistoryTimestamp(t time.Time) string {
	return t.Format("01/02 15:04")
}

// inTimezone converts a time to the configured global timezone so that history
// timestamps are consistent with what the LLM believes the current time is.
// Falls back to UTC when no valid timezone is configured.
func (h *Handler) inTimezone(t time.Time) time.Time {
	tz := h.chatService.GetTimezone()
	if tz == "" || tz == "auto" {
		return t.Local()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return t.UTC()
	}
	return t.In(loc)
}

// isBotAuthor returns true if the message author is the bot itself.
func isBotAuthor(s *discordgo.Session, author *discordgo.User) bool {
	if s == nil || s.State == nil || s.State.User == nil || author == nil {
		return false
	}
	return author.ID == s.State.User.ID
}
