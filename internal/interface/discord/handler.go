package discord

import (
	"context"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logHandler = logger.New("discord", logger.Magenta)

const (
	discordMaxLength         = 2000
	llmTimeout               = 300 * time.Second
	defaultGroupHistoryLimit = 24
	defaultHistoryTarget     = 12
)

// Handler bridges Discord events to the ChatService.
type Handler struct {
	bot                          *discordbot.Bot
	chatService                  *app.ChatService
	schedulerService             *schedulerpkg.Service
	convStore                    store.ConversationRepository
	useReply                     bool
	stripLeadingBotMentionPrefix bool
}

func NewHandler(
	bot *discordbot.Bot,
	chatService *app.ChatService,
	schedulerService *schedulerpkg.Service,
	convStore store.ConversationRepository,
	useReply bool,
	stripLeadingBotMentionPrefix bool,
) *Handler {
	return &Handler{
		bot:                          bot,
		chatService:                  chatService,
		schedulerService:             schedulerService,
		convStore:                    convStore,
		useReply:                     useReply,
		stripLeadingBotMentionPrefix: stripLeadingBotMentionPrefix,
	}
}

// Register attaches the message handler to the Discord session.
func (h *Handler) Register() {
	h.bot.Session().AddHandler(h.onMessageCreate)
}

// keepTyping sends typing indicator every 5 seconds until stopped.
func (h *Handler) keepTyping(ctx context.Context, channelID string) func() {
	ticker := time.NewTicker(5 * time.Second)
	done := make(chan struct{})

	_ = h.bot.SendTyping(channelID)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = h.bot.SendTyping(channelID)
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}

func (h *Handler) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	content, ok := h.filterIncomingMessage(s, m)
	if !ok {
		return
	}

	personaID := h.bot.PersonaID()
	ctx, cancel := h.newDiscordRequestContext(m, personaID)
	defer cancel()

	if h.tryHandleSchedulerStrict(ctx, m, personaID, content) {
		return
	}

	historyCutoff, err := h.loadHistoryCutoff(ctx, m.ChannelID, personaID)
	if err != nil {
		logHandler.Errorf("conversation state load failed [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, "❌ 讀取對話狀態失敗，請稍後再試。")
		return
	}

	stopTyping := h.keepTyping(ctx, m.ChannelID)
	defer stopTyping()

	images := h.prepareInboundImages(m.Attachments, m.ChannelID)
	messageToStore, historyEntries, contextForLLM, hasHistoryImages := h.buildInboundContext(s, m, personaID, content, historyCutoff)
	streamCh, err := h.startMessageStream(ctx, m.ChannelID, personaID, contextForLLM, messageToStore, images, historyEntries, hasHistoryImages)
	if err != nil {
		logHandler.Errorf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, formatDiscordError(err))
		return
	}

	h.streamToDiscord(m.ChannelID, m.ID, streamCh)
}
