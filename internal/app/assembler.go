package app

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/util"
)

// AssembledContext is the final prompt payload ready to send to a Provider.
type AssembledContext struct {
	SystemPrompt string
	Messages     []domain.Message
	Generation   domain.GenerationConfig
}

// ContextAssembler builds the final prompt from a PersonaModule + Conversation,
// choosing the topology based on model capabilities.
type ContextAssembler struct{}

func NewContextAssembler() *ContextAssembler {
	return &ContextAssembler{}
}

// Assemble builds the complete context for an LLM call.
// For thinking models: system_template (with lore+anchors injected) + raw history
// For legacy models: same system + anchors as few-shot message pairs + recent N + reinforcement
func (a *ContextAssembler) Assemble(
	persona *domain.PersonaModule,
	conv *domain.Conversation,
	caps llm.ModelCapabilities,
) (*AssembledContext, error) {
	systemPrompt, err := a.renderSystemPrompt(persona)
	if err != nil {
		return nil, fmt.Errorf("render system prompt: %w", err)
	}

	var messages []domain.Message

	if caps.SupportsThinking {
		messages = a.assembleThinkingModel(persona, conv)
	} else {
		messages = a.assembleLegacyModel(persona, conv)
	}

	return &AssembledContext{
		SystemPrompt: systemPrompt,
		Messages:     messages,
		Generation:   persona.Config.Generation,
	}, nil
}

// renderSystemPrompt executes the Go template in system_template,
// injecting Lore, formatted Anchors, and timezone (static for prompt caching).
func (a *ContextAssembler) renderSystemPrompt(persona *domain.PersonaModule) (string, error) {
	tmpl, err := template.New("system").Parse(persona.Config.SystemTemplate)
	if err != nil {
		return "", fmt.Errorf("parse system template: %w", err)
	}

	anchorsText := formatAnchorsForTemplate(persona.Anchors)

	// Determine timezone (persona-level overrides global, default to "auto")
	timezone := persona.Config.Timezone
	if timezone == "" {
		timezone = "auto" // Will be resolved to local timezone
	}

	data := map[string]string{
		"Lore":    persona.Lore,
		"Anchors": anchorsText,
		"Time":    util.FormatTimezoneOnly(timezone), // Static timezone string (cache-friendly)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute system template: %w", err)
	}

	// Append system-level tool usage instructions (non-persona-specific)
	buf.WriteString("\n\n---\n\n")
	buf.WriteString("## System Instructions\n\n")
	buf.WriteString("- Message envelopes contain timestamps. Use them for context about when messages were sent.\n")
	buf.WriteString("- If you need the current date, time, or day of week, call the `session_status` tool.\n")
	buf.WriteString("- Only call `session_status` when you need the precise current time, not for every response.\n")

	return buf.String(), nil
}

// assembleThinkingModel: anchors are already in the system_template via {{.Anchors}},
// so messages are just the raw conversation history.
func (a *ContextAssembler) assembleThinkingModel(
	_ *domain.PersonaModule,
	conv *domain.Conversation,
) []domain.Message {
	// Thinking models read the full raw history — their attention is strong enough
	return conv.Messages
}

// assembleLegacyModel: anchors as explicit few-shot message pairs,
// then recent conversation, then a reinforcement reminder.
func (a *ContextAssembler) assembleLegacyModel(
	persona *domain.PersonaModule,
	conv *domain.Conversation,
) []domain.Message {
	var messages []domain.Message

	// Anchors as explicit few-shot pairs (strongest anti-drift for legacy models)
	for _, anchor := range persona.Anchors {
		messages = append(messages,
			domain.Message{Role: domain.RoleUser, Content: anchor.UserInput},
			domain.Message{Role: domain.RoleAssistant, Content: anchor.BotResponse},
		)
	}

	// Recent conversation history (rolling window)
	const recentWindowSize = 20
	history := conv.Messages
	if len(history) > recentWindowSize {
		history = history[len(history)-recentWindowSize:]
	}
	messages = append(messages, history...)

	return messages
}

// formatAnchorsForTemplate renders anchors as readable text for {{.Anchors}} injection.
func formatAnchorsForTemplate(anchors []domain.Anchor) string {
	if len(anchors) == 0 {
		return "(No anchors defined)"
	}

	var sb strings.Builder
	for i, anchor := range anchors {
		sb.WriteString(fmt.Sprintf("### Example %d\n", i+1))
		sb.WriteString(fmt.Sprintf("**User:** %s\n", anchor.UserInput))
		sb.WriteString(fmt.Sprintf("**Response:**\n%s\n\n", anchor.BotResponse))
	}
	return sb.String()
}
