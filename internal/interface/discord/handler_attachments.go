package discord

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// isImageAttachment checks if a Discord attachment is an image.
func isImageAttachment(attachment *discordgo.MessageAttachment) bool {
	contentType := strings.ToLower(attachment.ContentType)
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	filename := strings.ToLower(attachment.Filename)
	return strings.HasSuffix(filename, ".jpg") ||
		strings.HasSuffix(filename, ".jpeg") ||
		strings.HasSuffix(filename, ".png") ||
		strings.HasSuffix(filename, ".gif") ||
		strings.HasSuffix(filename, ".webp") ||
		strings.HasSuffix(filename, ".bmp")
}

// buildAttachmentPlaceholder builds openclaw-style attachment placeholder.
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

	label := "file"
	tag := "<media:document>"
	if allImages {
		label = "image"
		tag = "<media:image>"
	}

	suffix := label
	if count > 1 {
		suffix = label + "s"
	}

	return fmt.Sprintf("%s (%d %s)", tag, count, suffix)
}

// buildAttachmentNarration builds a user-facing text for attachment-only input
// so the LLM can clearly identify who sent the image/file.
func buildAttachmentNarration(author *discordgo.User, attachments []*discordgo.MessageAttachment) string {
	if len(attachments) == 0 {
		return ""
	}

	sender := strings.TrimSpace(resolveSenderLabel(author))
	if sender == "" {
		sender = "使用者"
	}

	count := len(attachments)
	allImages := true
	for _, att := range attachments {
		if !isImageAttachment(att) {
			allImages = false
			break
		}
	}

	var action string
	switch {
	case allImages && count == 1:
		action = "傳送了 1 張圖片"
	case allImages && count > 1:
		action = fmt.Sprintf("傳送了 %d 張圖片", count)
	case count == 1:
		action = "傳送了 1 個附件"
	default:
		action = fmt.Sprintf("傳送了 %d 個附件", count)
	}

	// Keep OpenClaw-style placeholder for compatibility while adding explicit sender narrative.
	return fmt.Sprintf("%s %s。%s", sender, action, buildAttachmentPlaceholder(attachments))
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
		mimeType = "image/jpeg"
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
