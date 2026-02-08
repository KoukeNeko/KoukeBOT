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
	// Channel history limit (same as openclaw's DEFAULT_GROUP_HISTORY_LIMIT)
	defaultGroupHistoryLimit = 50
	// Context markers (same as openclaw)
	historyContextMarker  = "[Chat messages since your last reply - for context]"
	currentMessageMarker  = "[Current message]"
)

// Handler bridges Discord events to the ChatService.
type Handler struct {
	bot         *discordbot.Bot
	chatService *app.ChatService
	useReply    bool // Whether to use Discord reply feature
}

func NewHandler(bot *discordbot.Bot, chatService *app.ChatService, useReply bool) *Handler {
	return &Handler{
		bot:         bot,
		chatService: chatService,
		useReply:    useReply,
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

	// If no text content, use attachment placeholder (openclaw style)
	if strings.TrimSpace(content) == "" && len(m.Attachments) > 0 {
		content = buildAttachmentPlaceholder(m.Attachments)
	}

	// Skip if still empty after placeholder
	if strings.TrimSpace(content) == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	// Start typing indicator and keep it alive until response is sent
	stopTyping := h.keepTyping(ctx, m.ChannelID)
	defer stopTyping()

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

	// Format current message with envelope (openclaw style)
	senderLabel := resolveSenderLabel(m.Author)
	timestamp := m.Timestamp.Format("2006-01-02 15:04")
	currentMessage := formatInboundEnvelope("Discord", senderLabel, timestamp, content)

	// Build channel history context for LLM (openclaw style)
	// This is used for immediate context but NOT stored in conversation
	var contextForLLM string
	if !isDM(m) {
		contextForLLM = h.buildChannelHistoryContext(s, m.ChannelID, m.ID, currentMessage)
	} else {
		contextForLLM = currentMessage
	}

	// Pass both: contextForLLM (with channel history) and currentMessage (for storage)
	var streamCh <-chan llm.StreamChunk
	var err error
	if len(images) > 0 {
		streamCh, err = h.chatService.HandleMessageStreamWithImagesAndContext(
			ctx, m.ChannelID, h.bot.PersonaID(), contextForLLM, currentMessage, images)
	} else {
		streamCh, err = h.chatService.HandleMessageStreamWithContext(
			ctx, m.ChannelID, h.bot.PersonaID(), contextForLLM, currentMessage)
	}

	if err != nil {
		log.Printf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, "⚠️ Internal error. Check logs.")
		return
	}

	h.streamToDiscord(m.ChannelID, m.ID, streamCh)
}

// streamToDiscord collects streaming chunks and updates the Discord message in real-time.
func (h *Handler) streamToDiscord(channelID, replyToMessageID string, streamCh <-chan llm.StreamChunk) {
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

		// Handle tool calls (display indicator) - legacy support
		if len(chunk.ToolCalls) > 0 {
			toolIndicator := formatToolCallIndicator(chunk.ToolCalls)
			if messageID == "" {
				// Create initial message with tool indicator (use reply if enabled)
				var msg *discordgo.Message
				var err error
				if h.useReply {
					msg, err = h.bot.SendMessageReply(channelID, toolIndicator, replyToMessageID)
				} else {
					msg, err = h.bot.SendMessageWithReturn(channelID, toolIndicator)
				}
				if err != nil {
					log.Printf("send tool indicator error [%s]: %v", channelID, err)
				} else {
					messageID = msg.ID
				}
			} else {
				// Update existing message with tool indicator
				currentContent := fullResponse.String()
				if currentContent != "" {
					_ = h.bot.EditMessage(channelID, messageID, currentContent+"\n\n"+toolIndicator)
				} else {
					_ = h.bot.EditMessage(channelID, messageID, toolIndicator)
				}
			}
			continue
		}

		// Handle tool status updates (new approach)
		if chunk.ToolStatus != "" {
			if chunk.ToolStatus == "tool_start" {
				// Format tool status message
				var toolMsg string
				if chunk.ToolName == "web_search" {
					query := ""
					if chunk.ToolInput != nil {
						if q, ok := chunk.ToolInput["query"].(string); ok {
							query = q
						}
					}
					if query != "" {
						toolMsg = fmt.Sprintf("🔍 正在搜尋：%s", query)
					} else {
						toolMsg = "🔍 正在搜尋..."
					}
				} else if chunk.ToolName == "web_fetch" {
					url := ""
					if chunk.ToolInput != nil {
						if u, ok := chunk.ToolInput["url"].(string); ok {
							url = u
						}
					}
					if url != "" {
						toolMsg = fmt.Sprintf("📄 正在擷取網頁：%s", url)
					} else {
						toolMsg = "📄 正在擷取網頁..."
					}
				} else {
					toolMsg = fmt.Sprintf("🔧 正在使用工具：%s", chunk.ToolName)
				}

				if messageID == "" {
					// Create initial message with tool status (use reply if enabled)
					var msg *discordgo.Message
					var err error
					if h.useReply {
						msg, err = h.bot.SendMessageReply(channelID, toolMsg, replyToMessageID)
					} else {
						msg, err = h.bot.SendMessageWithReturn(channelID, toolMsg)
					}
					if err != nil {
						log.Printf("send tool status error [%s]: %v", channelID, err)
					} else {
						messageID = msg.ID
					}
				} else {
					// Update existing message with tool status
					_ = h.bot.EditMessage(channelID, messageID, toolMsg)
				}
			} else if chunk.ToolStatus == "tool_done" {
				// Tool execution done, ready for LLM response
				// Clear the accumulated response to prepare for final output
				fullResponse.Reset()
			}
			continue
		}

		// Accumulate content
		fullResponse.WriteString(chunk.Content)

		// If stream is done, break to final update
		if chunk.Done {
			break
		}

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
			// First update: create message (use reply if enabled)
			var msg *discordgo.Message
			var err error
			if h.useReply {
				msg, err = h.bot.SendMessageReply(channelID, segments[0], replyToMessageID)
			} else {
				msg, err = h.bot.SendMessageWithReturn(channelID, segments[0])
			}
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
		// Fallback: send all segments (use reply for first segment if enabled)
		if h.useReply && len(segments) > 0 {
			msg, err := h.bot.SendMessageReply(channelID, segments[0], replyToMessageID)
			if err == nil {
				messageID = msg.ID
			}
			// Send remaining segments without reply
			for i := 1; i < len(segments); i++ {
				_ = h.bot.SendMessage(channelID, segments[i])
			}
		} else {
			for _, segment := range segments {
				_ = h.bot.SendMessage(channelID, segment)
			}
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
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	// Fallback: check file extension
	filename := strings.ToLower(attachment.Filename)
	return strings.HasSuffix(filename, ".jpg") ||
		strings.HasSuffix(filename, ".jpeg") ||
		strings.HasSuffix(filename, ".png") ||
		strings.HasSuffix(filename, ".gif") ||
		strings.HasSuffix(filename, ".webp") ||
		strings.HasSuffix(filename, ".bmp")
}

// buildAttachmentPlaceholder builds openclaw-style attachment placeholder.
// Examples: "<media:image>", "<media:image> (3 images)", "<media:document> (2 files)"
func buildAttachmentPlaceholder(attachments []*discordgo.MessageAttachment) string {
	if len(attachments) == 0 {
		return ""
	}

	count := len(attachments)
	allImages := true
	for _, att := range attachments {
		if !isImageAttachment(att) {
			allImages = false
			break
		}
	}

	// Determine label and tag based on attachment types
	label := "file"
	tag := "<media:document>"
	if allImages {
		label = "image"
		tag = "<media:image>"
	}

	// Build suffix: "image" or "images" (openclaw style)
	suffix := label
	if count > 1 {
		suffix = label + "s"
	}

	return fmt.Sprintf("%s (%d %s)", tag, count, suffix)
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

// resolveSenderLabel returns a human-readable label for the message author.
// Priority: GlobalName > Username (with discriminator if present).
func resolveSenderLabel(author *discordgo.User) string {
	if author == nil {
		return ""
	}

	// Prefer GlobalName (display name) if available
	displayName := strings.TrimSpace(author.GlobalName)
	if displayName == "" {
		displayName = strings.TrimSpace(author.Username)
	}

	// If no valid name found, fallback to ID
	if displayName == "" {
		return author.ID
	}

	// Add discriminator if present and not "0" (Discord's new username system)
	discriminator := strings.TrimSpace(author.Discriminator)
	if discriminator != "" && discriminator != "0" {
		return fmt.Sprintf("%s#%s", displayName, discriminator)
	}

	return displayName
}

// formatInboundEnvelope formats a message with openclaw-style envelope.
// Format: [Discord sender timestamp] body
//         [from: sender]
func formatInboundEnvelope(channel, sender, timestamp, body string) string {
	envelope := fmt.Sprintf("[%s %s %s] %s", channel, sender, timestamp, body)
	if sender != "" {
		envelope = envelope + fmt.Sprintf("\n[from: %s]", sender)
	}
	return envelope
}

// formatToolCallIndicator formats tool call information for Discord display.
func formatToolCallIndicator(toolCalls []domain.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	var indicators []string
	for _, call := range toolCalls {
		switch call.Name {
		case "web_search":
			query := ""
			if q, ok := call.Input["query"].(string); ok {
				query = q
			}
			if query != "" {
				indicators = append(indicators, fmt.Sprintf("🔍 正在搜尋: %s", query))
			} else {
				indicators = append(indicators, "🔍 正在搜尋...")
			}
		default:
			indicators = append(indicators, fmt.Sprintf("🔧 使用工具: %s", call.Name))
		}
	}

	return strings.Join(indicators, "\n")
}

// buildChannelHistoryContext builds openclaw-style channel history context.
// Format:
// [Chat messages since your last reply - for context]
// [Discord Alice 2024-01-01 10:25] message 1
// [from: Alice]
// [Discord Bob 2024-01-01 10:27] message 2
// [from: Bob]
//
// [Current message]
// [Discord Alice 2024-01-01 10:30] current message
// [from: Alice]
func (h *Handler) buildChannelHistoryContext(s *discordgo.Session, channelID, currentMessageID, currentMessage string) string {
	messages, err := s.ChannelMessages(channelID, defaultGroupHistoryLimit, currentMessageID, "", "")
	if err != nil {
		log.Printf("failed to fetch channel history [%s]: %v", channelID, err)
		return currentMessage // Fallback to current message only
	}

	// If no history, return current message only
	if len(messages) == 0 {
		return currentMessage
	}

	// Build history entries (reverse order - oldest first)
	var historyLines []string
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		content := msg.Content

		// If no text content, use attachment placeholder (openclaw style)
		if strings.TrimSpace(content) == "" && len(msg.Attachments) > 0 {
			content = buildAttachmentPlaceholder(msg.Attachments)
		}

		// Skip empty messages (no text and no attachments)
		if strings.TrimSpace(content) == "" {
			continue
		}

		// Truncate long messages to avoid excessive token usage
		if len(content) > 200 {
			content = content[:200] + "..."
		}

		// Format with envelope
		senderLabel := resolveSenderLabel(msg.Author)
		timestamp := msg.Timestamp.Format("2006-01-02 15:04")
		formattedMsg := formatInboundEnvelope("Discord", senderLabel, timestamp, content)
		historyLines = append(historyLines, formattedMsg)
	}

	// Build final context with openclaw markers
	var result strings.Builder
	result.WriteString(historyContextMarker)
	result.WriteString("\n")
	result.WriteString(strings.Join(historyLines, "\n"))
	result.WriteString("\n\n")
	result.WriteString(currentMessageMarker)
	result.WriteString("\n")
	result.WriteString(currentMessage)

	return result.String()
}
