package discord

import (
	"context"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func (h *Handler) filterIncomingMessage(s *discordgo.Session, m *discordgo.MessageCreate) (string, bool) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return "", false
	}
	if !isMentioned(s, m) && !isDM(m) {
		return "", false
	}

	content := cleanMention(s, m, h.stripLeadingBotMentionPrefix)
	if strings.TrimSpace(content) == "" && len(m.Attachments) > 0 {
		content = buildAttachmentPlaceholder(m.Attachments)
	}
	if strings.TrimSpace(content) == "" {
		return "", false
	}
	return content, true
}

func (h *Handler) newDiscordRequestContext(m *discordgo.MessageCreate, personaID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	ctx = llm.WithRequestContext(ctx, llm.RequestContext{
		Source:    llm.RequestSourceDiscord,
		GuildID:   m.GuildID,
		ChannelID: m.ChannelID,
		UserID:    m.Author.ID,
		PersonaID: personaID,
	})
	return ctx, cancel
}

func (h *Handler) loadHistoryCutoff(ctx context.Context, channelID, personaID string) (*time.Time, error) {
	if h.convStore == nil {
		return nil, nil
	}
	conv, err := h.convStore.Get(ctx, channelID, personaID)
	if err != nil {
		return nil, err
	}
	if conv == nil || conv.HistoryCutoffAt == nil {
		return nil, nil
	}
	cutoff := conv.HistoryCutoffAt.UTC()
	return &cutoff, nil
}

func (h *Handler) prepareInboundImages(attachments []*discordgo.MessageAttachment, channelID string) []domain.ImageInput {
	var images []domain.ImageInput
	for _, attachment := range attachments {
		if len(images) >= maxCurrentMessageImages {
			logHandler.Warnf(
				"inbound images truncated [%s]: max=%d",
				channelID,
				maxCurrentMessageImages,
			)
			break
		}
		if !isImageAttachment(attachment) {
			continue
		}
		logHandler.Logf("downloading image: %s (%s, %d bytes)", attachment.Filename, attachment.ContentType, attachment.Size)
		img, err := downloadImage(attachment.URL)
		if err != nil {
			logHandler.Warnf("failed to download image [%s]: %v", channelID, err)
			continue
		}
		images = append(images, img)
	}
	if len(images) > 0 {
		logHandler.Logf("images ready: %d file(s)", len(images))
	}
	return images
}

func (h *Handler) buildInboundContext(
	s *discordgo.Session,
	m *discordgo.MessageCreate,
	personaID string,
	content string,
	historyCutoff *time.Time,
) (messageToStore string, ephemeralHistory []domain.Message, currentMessage string, historyEntries []domain.ChannelHistoryEntry, hasHistoryImages bool) {
	messageToStore = content

	// Label the current message with sender identity so the LLM can distinguish
	// who is speaking. Channel history messages carry "[Name · HH:mm]" labels;
	// the current message must use the same format so the LLM is not left guessing.
	senderLabel := resolveSenderLabel(m.Author)
	ts := formatHistoryTimestamp(h.inTimezone(m.Timestamp))
	currentMessage = "[" + senderLabel + " · " + ts + "] " + content

	if !isDM(m) {
		historyEntries = h.buildChannelHistoryEntries(s, m.ChannelID, personaID, m.ID, historyCutoff)
	}
	if replyEntry := h.buildReplyReferenceEntry(s, m, personaID, historyCutoff); replyEntry != nil {
		historyEntries = append(historyEntries, *replyEntry)
	}

	ephemeralHistory = buildEphemeralHistory(historyEntries)

	for _, entry := range historyEntries {
		if len(entry.Images) > 0 {
			hasHistoryImages = true
			break
		}
	}
	return
}

func (h *Handler) startMessageStream(
	ctx context.Context,
	channelID, personaID, currentMessage, messageToStore string,
	images []domain.ImageInput,
	historyEntries []domain.ChannelHistoryEntry,
	ephemeralHistory []domain.Message,
	hasHistoryImages bool,
) (<-chan llm.StreamChunk, error) {
	if len(images) > 0 || hasHistoryImages {
		return h.chatService.HandleMessageStreamWithNativeHistoryAndImages(
			ctx, channelID, personaID, ephemeralHistory, currentMessage, messageToStore, images, historyEntries)
	}
	return h.chatService.HandleMessageStreamWithNativeHistory(
		ctx, channelID, personaID, ephemeralHistory, currentMessage, messageToStore)
}
