package discord

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const (
	// Discord message character limit
	discordMaxLength = 2000
	// Timeout for a single LLM call
	llmTimeout = 120 * time.Second
)

// Handler bridges Discord events to the ChatService.
type Handler struct {
	bot         *discordbot.Bot
	chatService *app.ChatService
}

func NewHandler(bot *discordbot.Bot, chatService *app.ChatService) *Handler {
	return &Handler{
		bot:         bot,
		chatService: chatService,
	}
}

// Register attaches the message handler to the Discord session.
func (h *Handler) Register() {
	h.bot.Session().AddHandler(h.onMessageCreate)
}

func (h *Handler) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore own messages
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Ignore bot messages
	if m.Author.Bot {
		return
	}

	// Only respond when mentioned or in DMs
	if !isMentioned(s, m) && !isDM(m) {
		return
	}

	content := cleanMention(s, m.Content)
	if strings.TrimSpace(content) == "" {
		return
	}

	// Show typing indicator while processing
	_ = h.bot.SendTyping(m.ChannelID)

	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	streamCh, err := h.chatService.HandleMessageStream(ctx, m.ChannelID, h.bot.PersonaID(), content)
	if err != nil {
		log.Printf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, "⚠️ Internal error. Check logs.")
		return
	}

	h.streamToDiscord(m.ChannelID, streamCh)
}

// streamToDiscord collects streaming chunks and sends the result.
// Splits into multiple messages if exceeding Discord's 2000 char limit.
func (h *Handler) streamToDiscord(channelID string, streamCh <-chan llm.StreamChunk) {
	var fullResponse strings.Builder

	for chunk := range streamCh {
		if chunk.Error != nil {
			log.Printf("stream error [%s]: %v", channelID, chunk.Error)
			_ = h.bot.SendMessage(channelID, "⚠️ Stream error occurred.")
			return
		}
		fullResponse.WriteString(chunk.Content)
	}

	response := fullResponse.String()
	if response == "" {
		_ = h.bot.SendMessage(channelID, "(No response generated)")
		return
	}

	// Split and send if exceeding Discord limit
	for _, segment := range splitMessage(response, discordMaxLength) {
		if err := h.bot.SendMessage(channelID, segment); err != nil {
			log.Printf("send error [%s]: %v", channelID, err)
			return
		}
	}
}

// isMentioned checks if the bot was @mentioned in the message.
func isMentioned(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	for _, user := range m.Mentions {
		if user.ID == s.State.User.ID {
			return true
		}
	}
	return false
}

// isDM checks if the message is a direct message.
func isDM(m *discordgo.MessageCreate) bool {
	return m.GuildID == ""
}

// cleanMention strips the bot's @mention from the message content.
func cleanMention(s *discordgo.Session, content string) string {
	content = strings.ReplaceAll(content, "<@"+s.State.User.ID+">", "")
	content = strings.ReplaceAll(content, "<@!"+s.State.User.ID+">", "")
	return strings.TrimSpace(content)
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

		// Try to split at a newline near the limit
		cutPoint := strings.LastIndex(text[:maxLen], "\n")
		if cutPoint <= 0 {
			cutPoint = maxLen
		}

		segments = append(segments, text[:cutPoint])
		text = text[cutPoint:]
		// Trim leading newline from next segment
		text = strings.TrimPrefix(text, "\n")
	}

	return segments
}
