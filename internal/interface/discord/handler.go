package discord

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	interfacecommon "github.com/KoukeNeko/KoukeBOT/internal/interface/shared"
)

var logHandler = logger.New("discord", logger.Magenta)

const (
	// Discord message character limit
	discordMaxLength = 2000
	// Timeout for a single LLM call
	llmTimeout = 300 * time.Second
	// Channel history limit (same as openclaw's DEFAULT_GROUP_HISTORY_LIMIT)
	defaultGroupHistoryLimit = 24
	defaultHistoryTarget     = 12
	// Context markers — re-exported from domain for local use
	historyContextMarker = domain.HistoryContextMarker
	currentMessageMarker = domain.CurrentMessageMarker
)

// Handler bridges Discord events to the ChatService.
type Handler struct {
	bot         *discordbot.Bot
	chatService *app.ChatService
	convStore   store.ConversationRepository
	useReply    bool // Whether to use Discord reply feature
}

func NewHandler(bot *discordbot.Bot, chatService *app.ChatService, convStore store.ConversationRepository, useReply bool) *Handler {
	return &Handler{
		bot:         bot,
		chatService: chatService,
		convStore:   convStore,
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
	content, ok := h.filterIncomingMessage(s, m)
	if !ok {
		return
	}

	ctx, cancel := h.newDiscordRequestContext(m)
	defer cancel()

	personaID := h.bot.PersonaID()
	historyCutoff, err := h.loadHistoryCutoff(ctx, m.ChannelID, personaID)
	if err != nil {
		logHandler.Errorf("conversation state load failed [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, "❌ 讀取對話狀態失敗，請稍後再試。")
		return
	}

	stopTyping := h.keepTyping(ctx, m.ChannelID)
	defer stopTyping()

	images := h.prepareInboundImages(m.Attachments, m.ChannelID)
	currentMessage, historyEntries, contextForLLM, hasHistoryImages := h.buildInboundContext(s, m, content, historyCutoff)
	streamCh, err := h.startMessageStream(ctx, m.ChannelID, personaID, contextForLLM, currentMessage, images, historyEntries, hasHistoryImages)
	if err != nil {
		logHandler.Errorf("chat error [%s]: %v", m.ChannelID, err)
		_ = h.bot.SendMessage(m.ChannelID, formatDiscordError(err))
		return
	}

	h.streamToDiscord(m.ChannelID, m.ID, streamCh)
}

// buildReplyReferenceEntry extracts the replied-to message as high-priority context.
// If Discord did not include ReferencedMessage payload, it falls back to an API fetch.
func (h *Handler) buildReplyReferenceEntry(s *discordgo.Session, m *discordgo.MessageCreate, cutoff *time.Time) *domain.ChannelHistoryEntry {
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
	timestamp := refMsg.Timestamp.Format("2006-01-02 15:04 MST")
	formatted := "[Replied message - high priority]\n" +
		formatInboundEnvelope("Discord", senderLabel, userID, timestamp, content)

	// Include at most 2 images from the replied message so multimodal models
	// can resolve "this image" references without exploding context cost.
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

	return &domain.ChannelHistoryEntry{
		FormattedText: formatted,
		Images:        images,
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
// Format: [Discord sender (id: userID) timestamp] body
//
//	[from: sender (id: userID)]
func formatInboundEnvelope(channel, sender, userID, timestamp, body string) string {
	envelope := fmt.Sprintf("[%s %s (id: %s) %s] %s", channel, sender, userID, timestamp, body)
	if sender != "" {
		envelope = envelope + fmt.Sprintf("\n[from: %s (id: %s)]", sender, userID)
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

func formatToolStatusError(toolName string, toolInput map[string]interface{}, rawErr string) string {
	errText := strings.TrimSpace(rawErr)
	if errText == "" {
		errText = "未知錯誤"
	}
	errText = truncateError(errText, 220)
	lowerErr := strings.ToLower(errText)

	if strings.HasPrefix(lowerErr, "tool guard:") {
		return fmt.Sprintf("⚠️ 工具保護機制觸發（%s）：%s", safeToolName(toolName), errText)
	}

	switch toolName {
	case "file_write":
		return fmt.Sprintf("❌ 儲存檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	case "file_append":
		return fmt.Sprintf("❌ 追加檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	case "file_read":
		return fmt.Sprintf("❌ 讀取檔案失敗（%s）：%s", extractToolFilename(toolInput), errText)
	default:
		if toolName == "" {
			toolName = "unknown"
		}
		return fmt.Sprintf("❌ 工具 %s 執行失敗：%s", toolName, errText)
	}
}

func formatToolStartStatusMessage(toolName string, toolInput map[string]interface{}) string {
	return interfacecommon.FormatToolStartMessage(toolName, toolInput)
}

func safeToolName(toolName string) string {
	if strings.TrimSpace(toolName) == "" {
		return "unknown"
	}
	return toolName
}

func extractToolFilename(toolInput map[string]interface{}) string {
	if toolInput == nil {
		return "unknown"
	}
	if v, ok := toolInput["filename"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := toolInput["file"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return "unknown"
}

// formatDiscordError converts technical errors to user-friendly Discord messages
func formatDiscordError(err error) string {
	parsed := interfacecommon.ParseLLMError(err)
	switch parsed.Kind {
	case interfacecommon.ErrorKindQuota:
		if parsed.ResetAfter != "" {
			return fmt.Sprintf("⏳ **API 配額已用完**\n請等待 **%s** 後重試\n\n💡 提示：可至 Web UI Settings 切換到 API Key 模式", parsed.ResetAfter)
		}
		return "⏳ **API 配額已用完**\n請稍後再試\n\n💡 提示：請至 Web UI Settings 切換到 API Key 模式，或等待配額重置"
	case interfacecommon.ErrorKindAuth:
		return "🔐 **認證失敗**\n請至 Web UI Settings 重新設定 API Key 或 OAuth"
	case interfacecommon.ErrorKindNetwork:
		return "🌐 **網路連線失敗**\n請檢查網路連線或稍後再試"
	case interfacecommon.ErrorKindRateLimit:
		return "⚠️ **請求過於頻繁**\n請稍等片刻後再試"
	}

	// Generic error fallback
	return fmt.Sprintf("❌ **發生錯誤**\n%s\n\n💡 請查看後端 log 或至 Web UI 檢查設定", truncateError(parsed.RawMessage, 200))
}

// truncateError truncates error message to avoid Discord message length limit
func truncateError(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}

// buildChannelHistoryEntries builds structured channel history entries with
// per-message image association so the LLM can see which image belongs to which
// message. Images from the most recent maxHistoryImageScan messages are
// downloaded and attached to their corresponding entry (up to maxHistoryImages
// total).
func (h *Handler) buildChannelHistoryEntries(s *discordgo.Session, channelID, currentMessageID string, cutoff *time.Time) []domain.ChannelHistoryEntry {
	const (
		maxHistoryImageScan = 10 // only scan last N messages for images
		maxHistoryImages    = 5  // collect at most this many images
		historyCharBudget   = 4500
	)

	messages, err := s.ChannelMessages(channelID, defaultGroupHistoryLimit, currentMessageID, "", "")
	if err != nil {
		logHandler.Warnf("failed to fetch channel history [%s]: %v", channelID, err)
		return nil
	}

	if len(messages) == 0 {
		return nil
	}

	// Build history entries (reverse order - oldest first)
	var entries []domain.ChannelHistoryEntry
	imageCount := 0
	usedChars := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !shouldIncludeDiscordTimestamp(msg.Timestamp, cutoff) {
			continue
		}
		content := msg.Content

		// If no text content, use attachment placeholder (openclaw style)
		if strings.TrimSpace(content) == "" && len(msg.Attachments) > 0 {
			content = buildAttachmentPlaceholder(msg.Attachments)
		}

		// Skip empty messages (no text and no attachments)
		if strings.TrimSpace(content) == "" {
			continue
		}

		// Collect images from recent messages (last maxHistoryImageScan msgs)
		var entryImages []domain.ImageInput
		if i < maxHistoryImageScan && imageCount < maxHistoryImages {
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

		// Dynamic truncation by remaining budget.
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

		// Format with envelope
		senderLabel := resolveSenderLabel(msg.Author)
		userID := ""
		if msg.Author != nil {
			userID = msg.Author.ID
		}
		timestamp := msg.Timestamp.Format("2006-01-02 15:04 MST")
		formattedMsg := formatInboundEnvelope("Discord", senderLabel, userID, timestamp, content)

		entries = append(entries, domain.ChannelHistoryEntry{
			FormattedText: formattedMsg,
			Images:        entryImages,
		})
		usedChars += len(formattedMsg)

		// Stop once we already have enough entries and budget is mostly consumed.
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

// buildContextForLLM rebuilds the flat context string from structured history
// entries. Used for the text-only code path and for conversation persistence.
func buildContextForLLM(entries []domain.ChannelHistoryEntry, currentMessage string) string {
	if len(entries) == 0 {
		return currentMessage
	}

	var result strings.Builder
	result.WriteString(historyContextMarker)
	result.WriteString("\n")
	for i, entry := range entries {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(entry.FormattedText)
	}
	result.WriteString("\n\n")
	result.WriteString(currentMessageMarker)
	result.WriteString("\n")
	result.WriteString(currentMessage)

	return result.String()
}
