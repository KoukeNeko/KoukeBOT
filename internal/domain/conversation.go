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
	Role             Role       `json:"role"`
	Content          string     `json:"content"`
	DisplayText      string     `json:"display_text,omitempty"`        // Full context for UI display (e.g., with channel history)
	RawLLMInput      string     `json:"raw_llm_input,omitempty"`       // Raw prompt sent to LLM (for debugging/transparency)
	RawLLMInputHuman string     `json:"raw_llm_input_human,omitempty"` // Humanized prompt view for debugging/transparency
	RawLLMOutput     string     `json:"raw_llm_output,omitempty"`      // Raw response from LLM (for debugging/transparency)
	Timestamp        time.Time  `json:"timestamp"`
	ImageRefs        []ImageRef `json:"image_refs,omitempty"` // References to persisted images
}

// ConversationDigest is a compact structured summary of older history.
// It allows token-efficient recall without replaying full raw turns.
type ConversationDigest struct {
	ID             string    `json:"id"`
	Summary        string    `json:"summary"`
	Facts          []string  `json:"facts,omitempty"`
	Preferences    []string  `json:"preferences,omitempty"`
	OpenItems      []string  `json:"open_items,omitempty"`
	Decisions      []string  `json:"decisions,omitempty"`
	Keywords       []string  `json:"keywords,omitempty"`
	Attitude       string    `json:"attitude,omitempty"`
	EmotionalTone  string    `json:"emotional_tone,omitempty"`
	StyleMarkers   []string  `json:"style_markers,omitempty"`
	SignatureTerms []string  `json:"signature_terms,omitempty"`
	StyleQuotes    []string  `json:"style_quotes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Conversation tracks the full dialogue state for a channel-persona pair.
type Conversation struct {
	ID               string               `json:"id"`
	ChannelID        string               `json:"channel_id"`
	PersonaID        string               `json:"persona_id"`
	Messages         []Message            `json:"messages"`
	ContextDigests   []ConversationDigest `json:"context_digests,omitempty"`
	HistoryCutoffAt  *time.Time           `json:"history_cutoff_at,omitempty"` // Discord /reset boundary: ignore history/replies at or before this time.
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
	CumulativeTokens int                  `json:"cumulative_tokens"`            // Estimated current prompt context footprint (for flush tracking)
	UsageTokensTotal int                  `json:"usage_tokens_total,omitempty"` // Lifetime token usage accumulated from provider responses
}

// AddUsageTokens adds token usage from a turn to the lifetime usage counter.
func (c *Conversation) AddUsageTokens(tokens int) {
	if tokens <= 0 {
		return
	}
	c.UsageTokensTotal += tokens
}

// UpdateTokenUsage is kept for backward compatibility with older call sites.
// New code should use AddUsageTokens and update CumulativeTokens separately.
func (c *Conversation) UpdateTokenUsage(tokens int) {
	c.AddUsageTokens(tokens)
}

// AddDigest appends a digest and caps retention to avoid unbounded growth.
func (c *Conversation) AddDigest(digest ConversationDigest) {
	const maxDigests = 64
	c.ContextDigests = append(c.ContextDigests, digest)
	if len(c.ContextDigests) > maxDigests {
		c.ContextDigests = c.ContextDigests[len(c.ContextDigests)-maxDigests:]
	}
	c.UpdatedAt = time.Now()
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
	c.AppendAssistantMessageWithDisplayAndRaw(content, content, rawInput, rawOutput)
}

// AppendAssistantMessageWithDisplayAndRaw adds an assistant response with display text and raw I/O.
func (c *Conversation) AppendAssistantMessageWithDisplayAndRaw(content, displayText, rawInput, rawOutput string) {
	c.AppendAssistantMessageWithDisplayAndRawViews(content, displayText, rawInput, rawInput, rawOutput)
}

// AppendAssistantMessageWithDisplayAndRawViews adds an assistant response with display text and
// both raw and humanized LLM input snapshots.
func (c *Conversation) AppendAssistantMessageWithDisplayAndRawViews(content, displayText, rawInput, rawInputHuman, rawOutput string) {
	c.Messages = append(c.Messages, Message{
		Role:             RoleAssistant,
		Content:          content,
		DisplayText:      displayText,
		RawLLMInput:      rawInput,
		RawLLMInputHuman: rawInputHuman,
		RawLLMOutput:     rawOutput,
		Timestamp:        time.Now(),
	})
	c.UpdatedAt = time.Now()
}

// Context markers for channel history formatting.
const (
	HistoryContextMarker = "[Chat messages since your last reply - for context]"
	CurrentMessageMarker = "[Current message]"
)

// ImageInput represents an uploaded image for vision analysis.
type ImageInput struct {
	Data     []byte // Raw image bytes
	MimeType string // e.g., "image/jpeg", "image/png"
	Filename string // Optional, for logging
}

// ChannelHistoryEntry represents a single message from Discord channel history
// with its associated images (if any).
type ChannelHistoryEntry struct {
	FormattedText string       // Envelope-formatted message text
	Images        []ImageInput // Images attached to this specific message
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
