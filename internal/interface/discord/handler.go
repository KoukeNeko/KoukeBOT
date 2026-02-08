package discord

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
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
	if strings.TrimSpace(content) == "" && len(m.Attachments) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	// Start typing indicator and keep it alive until response is sent
	stopTyping := h.keepTyping(ctx, m.ChannelID)
	defer stopTyping()

	// Fetch recent channel messages for context (only in guilds, not DMs)
	var channelContext string
	if !isDM(m) {
		channelContext = h.fetchChannelContext(s, m.ChannelID, m.ID, 10)
	}

	// Check for image attachments
	var images []domain.ImageInput
	for _, attachment := range m.Attachments {
		if isImageAttachment(attachment) {
			img, err := downloadImage(attachment.URL)
			if err != nil {
				log.Printf("failed to download image [%s]: %v", m.ChannelID, err)
				continue
			}
			images = append(images, img)
		}
	}

	// Prepend channel context if available
	finalContent := content
	if channelContext != "" {
		finalContent = channelContext + "\n" + content
	}

	// Use multimodal handler if images are present
	var streamCh <-chan llm.StreamChunk
	var err error
	if len(images) > 0 {
		if content == "" {
			content = "這張圖片顯示了什麼？" // Default prompt if no text provided
		}
		// For images, use original content without context to avoid confusion
		streamCh, err = h.chatService.HandleMessageStreamWithImages(ctx, m.ChannelID, h.bot.PersonaID(), content, images)
	} else {
		streamCh, err = h.chatService.HandleMessageStream(ctx, m.ChannelID, h.bot.PersonaID(), finalContent)
	}

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

// isImageAttachment checks if a Discord attachment is an image.
func isImageAttachment(attachment *discordgo.MessageAttachment) bool {
	contentType := strings.ToLower(attachment.ContentType)
	return strings.HasPrefix(contentType, "image/")
}

// downloadImage downloads an image from a URL and returns it as ImageInput.
func downloadImage(url string) (domain.ImageInput, error) {
	resp, err := http.Get(url)
	if err != nil {
		return domain.ImageInput{}, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.ImageInput{}, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ImageInput{}, fmt.Errorf("read body: %w", err)
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg" // default
	}

	return domain.ImageInput{
		Data:     data,
		MimeType: mimeType,
		Filename: extractFilename(url),
	}, nil
}

// extractFilename extracts filename from URL.
func extractFilename(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "image"
}

// fetchChannelContext fetches recent messages from the channel for context.
// This helps the bot understand the ongoing conversation in the channel.
func (h *Handler) fetchChannelContext(s *discordgo.Session, channelID, currentMessageID string, limit int) string {
	messages, err := s.ChannelMessages(channelID, limit, currentMessageID, "", "")
	if err != nil {
		log.Printf("failed to fetch channel history [%s]: %v", channelID, err)
		return ""
	}

	if len(messages) == 0 {
		return ""
	}

	// Build context string from recent messages (reverse order - oldest first)
	var contextLines []string
	contextLines = append(contextLines, "[Recent channel messages for context:]")

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		author := msg.Author.Username
		content := msg.Content

		// Skip empty messages
		if strings.TrimSpace(content) == "" {
			continue
		}

		// Truncate long messages
		if len(content) > 200 {
			content = content[:200] + "..."
		}

		contextLines = append(contextLines, fmt.Sprintf("- %s: %s", author, content))
	}

	contextLines = append(contextLines, "[End of context]\n")
	return strings.Join(contextLines, "\n")
}
