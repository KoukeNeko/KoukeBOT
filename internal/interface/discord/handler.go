package discord

import (
	"context"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logHandler = logger.New("discord", logger.Magenta)

const (
	discordMaxLength         = 2000
	llmTimeout               = 300 * time.Second
	defaultGroupHistoryLimit = 24
	defaultHistoryTarget     = 12
	maxCurrentMessageImages  = 3
	maxReplyReferenceImages  = 2
	maxHistoryImageScan      = 10
	maxHistoryImages         = 5
	historyCharBudget        = 4500
	// historyImageMaxAge limits which channel history images are sent to the LLM.
	// Images older than this threshold are excluded to prevent the model from
	// responding to stale images that lack surrounding conversation context.
	historyImageMaxAge = 30 * time.Minute
)

// seenMessageTTL is how long a processed message ID is remembered to deduplicate
// duplicate MESSAGE_CREATE events from Discord gateway reconnects.
const seenMessageTTL = 5 * time.Minute

// Handler bridges Discord events to the ChatService.
type Handler struct {
	bot                          *discordbot.Bot
	chatService                  *app.ChatService
	schedulerService             *schedulerpkg.Service
	convStore                    store.ConversationRepository
	useReply                     bool
	stripLeadingBotMentionPrefix bool

	seenMu  sync.Mutex
	seenIDs map[string]time.Time
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
		seenIDs:                      make(map[string]time.Time),
	}
}

// checkAndMarkSeen returns true if this message ID was already processed recently.
// Uses lazy expiry: expired entries are cleaned up on each check.
func (h *Handler) checkAndMarkSeen(messageID string) bool {
	h.seenMu.Lock()
	defer h.seenMu.Unlock()
	now := time.Now()
	for id, t := range h.seenIDs {
		if now.Sub(t) > seenMessageTTL {
			delete(h.seenIDs, id)
		}
	}
	if _, ok := h.seenIDs[messageID]; ok {
		return true
	}
	h.seenIDs[messageID] = now
	return false
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

func (h *Handler) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	content, ok := h.filterIncomingMessage(s, m)
	if !ok {
		return
	}
	if h.checkAndMarkSeen(m.ID) {
		logHandler.Warnf("duplicate MESSAGE_CREATE [%s], skipping", m.ID)
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
	messageToStore, ephemeralHistory, currentMessage, historyEntries, hasHistoryImages := h.buildInboundContext(s, m, personaID, content, historyCutoff)
	streamCh, err := h.startMessageStream(ctx, m.ChannelID, personaID, currentMessage, messageToStore, images, historyEntries, ephemeralHistory, hasHistoryImages)
	if err != nil {
		logHandler.Errorf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, formatDiscordError(err))
		return
	}

	h.streamToDiscord(m.ChannelID, m.ID, streamCh)
}
