package domain

import "time"

// Role identifies the speaker in a conversation turn.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ImageRef is a lightweight reference to an image stored on disk.
type ImageRef struct {
	StorageKey       string `json:"storage_key"`
	MimeType         string `json:"mime_type"`
	OriginalFilename string `json:"original_filename"`
}

// Message is a single turn in a conversation.
type Message struct {
	Role         Role       `json:"role"`
	Content      string     `json:"content"`
	DisplayText  string     `json:"display_text,omitempty"`   // Full context for UI display (e.g., with channel history)
	RawLLMInput  string     `json:"raw_llm_input,omitempty"`  // Raw prompt sent to LLM (for debugging/transparency)
	RawLLMOutput string     `json:"raw_llm_output,omitempty"` // Raw response from LLM (for debugging/transparency)
	Timestamp    time.Time  `json:"timestamp"`
	ImageRefs    []ImageRef `json:"image_refs,omitempty"`     // References to persisted images
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

// AppendUserMessageWithDisplay adds a user message with separate content and display text.
// content: clean message for LLM context assembly
// displayText: full context for UI display (e.g., with channel history markers)
func (c *Conversation) AppendUserMessageWithDisplay(content, displayText string) {
	c.Messages = append(c.Messages, Message{
		Role:        RoleUser,
		Content:     content,
		DisplayText: displayText,
		Timestamp:   time.Now(),
	})
	c.UpdatedAt = time.Now()
}

// AppendUserMessageWithImages adds a user message with image references.
func (c *Conversation) AppendUserMessageWithImages(content, displayText string, imageRefs []ImageRef) {
	c.Messages = append(c.Messages, Message{
		Role:        RoleUser,
		Content:     content,
		DisplayText: displayText,
		ImageRefs:   imageRefs,
		Timestamp:   time.Now(),
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

// AppendAssistantMessageWithRaw adds an assistant response with raw LLM input/output for debugging.
func (c *Conversation) AppendAssistantMessageWithRaw(content, rawInput, rawOutput string) {
	c.Messages = append(c.Messages, Message{
		Role:         RoleAssistant,
		Content:      content,
		RawLLMInput:  rawInput,
		RawLLMOutput: rawOutput,
		Timestamp:    time.Now(),
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
