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

// keepTyping sends typing indicator every 5 seconds until stopped.
// Returns a function to stop the typing indicator.
func (h *Handler) keepTyping(ctx context.Context, channelID string) func() {
	ticker := time.NewTicker(5 * time.Second)
	done := make(chan struct{})

	// Send initial typing indicator
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

	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	// Start typing indicator and keep it alive until response is sent
	stopTyping := h.keepTyping(ctx, m.ChannelID)
	defer stopTyping()

	streamCh, err := h.chatService.HandleMessageStream(ctx, m.ChannelID, h.bot.PersonaID(), content)
	if err != nil {
		log.Printf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, "⚠️ Internal error. Check logs.")
		return
	}

	h.streamToDiscord(m.ChannelID, streamCh)
}

// streamToDiscord collects streaming chunks and updates the Discord message in real-time.
func (h *Handler) streamToDiscord(channelID string, streamCh <-chan llm.StreamChunk) {
	var fullResponse strings.Builder
	var lastUpdate time.Time
	var messageID string

	// Update interval: 1.5 seconds (避免 rate limit)
	const updateInterval = 1500 * time.Millisecond

	for chunk := range streamCh {
		if chunk.Error != nil {
			log.Printf("stream error [%s]: %v", channelID, chunk.Error)

			if messageID != "" {
				// Update existing message with error
				_ = h.bot.EditMessage(channelID, messageID, "⚠️ Stream error occurred.")
			} else {
				_ = h.bot.SendMessage(channelID, "⚠️ Stream error occurred.")
			}
			return
		}

		fullResponse.WriteString(chunk.Content)

		// Throttle updates to avoid rate limiting
		if time.Since(lastUpdate) < updateInterval {
			continue
		}
		lastUpdate = time.Now()

		currentContent := fullResponse.String()
		if currentContent == "" {
			continue
		}

		// Split if exceeds Discord limit
		segments := splitMessage(currentContent, discordMaxLength)

		if messageID == "" {
			// First update: create message
			msg, err := h.bot.SendMessageWithReturn(channelID, segments[0])
			if err != nil {
				log.Printf("send error [%s]: %v", channelID, err)
				return
			}
			messageID = msg.ID
		} else {
			// Subsequent updates: edit message
			if err := h.bot.EditMessage(channelID, messageID, segments[0]); err != nil {
				log.Printf("edit error [%s]: %v", channelID, err)
				return
			}
		}
	}

	// Final update: ensure complete response is sent
	finalContent := fullResponse.String()
	if finalContent == "" {
		if messageID == "" {
			_ = h.bot.SendMessage(channelID, "(No response generated)")
		}
		return
	}

	segments := splitMessage(finalContent, discordMaxLength)

	if messageID != "" {
		// Update first message with final content
		_ = h.bot.EditMessage(channelID, messageID, segments[0])

		// Send additional segments if needed
		for i := 1; i < len(segments); i++ {
			_ = h.bot.SendMessage(channelID, segments[i])
		}
	} else {
		// Fallback: send all segments
		for _, segment := range segments {
			_ = h.bot.SendMessage(channelID, segment)
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
