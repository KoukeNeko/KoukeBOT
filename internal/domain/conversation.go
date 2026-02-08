package domain

import "time"

// Role identifies the speaker in a conversation turn.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is a single turn in a conversation.
type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Conversation tracks the full dialogue state for a channel-persona pair.
type Conversation struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	PersonaID string    `json:"persona_id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AppendUserMessage adds a user message and updates the timestamp.
func (c *Conversation) AppendUserMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	})
	c.UpdatedAt = time.Now()
}

// AppendAssistantMessage adds an assistant response and updates the timestamp.
func (c *Conversation) AppendAssistantMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      RoleAssistant,
		Content:   content,
		Timestamp: time.Now(),
	})
	c.UpdatedAt = time.Now()
}

// ImageInput represents an uploaded image for vision analysis.
type ImageInput struct {
	Data     []byte // Raw image bytes
	MimeType string // e.g., "image/jpeg", "image/png"
	Filename string // Optional, for logging
}

// MessagePart represents a part of a multimodal message.
type MessagePart struct {
	Text      string      // Text content (if any)
	ImageData *ImageInput // Image data (if any)
}

// MultimodalMessage extends Message with support for images and other media.
type MultimodalMessage struct {
	Role      Role
	Parts     []MessagePart
	Timestamp time.Time
}
