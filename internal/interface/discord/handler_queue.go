package discord

import (
	"github.com/bwmarrin/discordgo"
)

const (
	emojiQueued     = "👀"
	emojiProcessing = "🔄"
	emojiDone       = "☑️"
)

type queuedMessageTask struct {
	session *discordgo.Session
	message *discordgo.MessageCreate
	content string
}

func (h *Handler) startMessageQueueWorker() {
	if h == nil {
		return
	}

	h.queueOnce.Do(func() {
		if h.messageQueue == nil {
			h.messageQueue = make(chan queuedMessageTask, defaultMessageQueueSize)
		}
		go h.runMessageQueueWorker()
	})
}

func (h *Handler) enqueueMessageTask(s *discordgo.Session, m *discordgo.MessageCreate, content string) {
	if h == nil || m == nil {
		return
	}
	h.startMessageQueueWorker()

	h.addReaction(m.ChannelID, m.ID, emojiQueued)

	task := queuedMessageTask{
		session: s,
		message: m,
		content: content,
	}

	select {
	case h.messageQueue <- task:
		logHandler.Logf("queued message task [%s]: queue_depth=%d", m.ID, len(h.messageQueue))
	default:
		logHandler.Warnf("message queue full [%s], dropping task", m.ID)
		h.removeReaction(m.ChannelID, m.ID, emojiQueued)
		_ = h.bot.SendMessage(m.ChannelID, "⚠️ 目前排隊請求過多，請稍後再試。")
	}
}

func (h *Handler) runMessageQueueWorker() {
	for task := range h.messageQueue {
		h.transitionToProcessing(task.message)
		h.processQueuedMessage(task)
		h.transitionToDone(task.message)
	}
}

func (h *Handler) processQueuedMessage(task queuedMessageTask) {
	if task.message == nil {
		return
	}

	personaID := h.bot.PersonaID()
	ctx, cancel := h.newDiscordRequestContext(task.message, personaID)
	defer cancel()

	if h.tryHandleSchedulerStrict(ctx, task.message, personaID, task.content) {
		return
	}

	historyCutoff, err := h.loadHistoryCutoff(ctx, task.message.ChannelID, personaID)
	if err != nil {
		logHandler.Errorf("conversation state load failed [%s]: %v", task.message.ChannelID, err)
		_ = h.bot.SendMessage(task.message.ChannelID, "❌ 讀取對話狀態失敗，請稍後再試。")
		return
	}

	stopTyping := h.keepTyping(ctx, task.message.ChannelID)
	defer stopTyping()

	images := h.prepareInboundImages(task.message.Attachments, task.message.ChannelID)
	messageToStore, ephemeralHistory, currentMessage, historyEntries, hasHistoryImages :=
		h.buildInboundContext(task.session, task.message, personaID, task.content, historyCutoff)
	streamCh, err := h.startMessageStream(
		ctx,
		task.message.ChannelID,
		personaID,
		currentMessage,
		messageToStore,
		images,
		historyEntries,
		ephemeralHistory,
		hasHistoryImages,
	)
	if err != nil {
		logHandler.Errorf("chat error [%s]: %v", task.message.ChannelID, err)
		_ = h.bot.SendMessage(task.message.ChannelID, formatDiscordError(err))
		return
	}

	h.streamToDiscord(task.message.ChannelID, task.message.ID, streamCh)
}

func (h *Handler) transitionToProcessing(m *discordgo.MessageCreate) {
	if m == nil {
		return
	}
	h.removeReaction(m.ChannelID, m.ID, emojiQueued)
	h.addReaction(m.ChannelID, m.ID, emojiProcessing)
}

func (h *Handler) transitionToDone(m *discordgo.MessageCreate) {
	if m == nil {
		return
	}
	h.removeReaction(m.ChannelID, m.ID, emojiProcessing)
	h.addReaction(m.ChannelID, m.ID, emojiDone)
}

func (h *Handler) addReaction(channelID, messageID, emoji string) {
	if h == nil || h.bot == nil || h.bot.Session() == nil {
		return
	}
	if err := h.bot.Session().MessageReactionAdd(channelID, messageID, emoji); err != nil {
		logHandler.Warnf("reaction add failed [%s/%s %s]: %v", channelID, messageID, emoji, err)
	}
}

func (h *Handler) removeReaction(channelID, messageID, emoji string) {
	if h == nil || h.bot == nil || h.bot.Session() == nil || h.bot.Session().State == nil || h.bot.Session().State.User == nil {
		return
	}
	if err := h.bot.Session().MessageReactionRemove(channelID, messageID, emoji, h.bot.Session().State.User.ID); err != nil {
		logHandler.Warnf("reaction remove failed [%s/%s %s]: %v", channelID, messageID, emoji, err)
	}
}
