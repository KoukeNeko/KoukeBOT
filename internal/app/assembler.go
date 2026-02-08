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

	// Inject UserProfile (if exists)
	if persona.UserProfile != "" {
		buf.WriteString("\n\n---\n\n")
		buf.WriteString("## About Your User\n\n")
		buf.WriteString(persona.UserProfile)
	}

	// Inject LongTermMemory (if exists)
	if persona.LongTermMemory != "" {
		buf.WriteString("\n\n---\n\n")
		buf.WriteString("## Your Long-Term Memory\n\n")
		buf.WriteString(persona.LongTermMemory)
	}

	// Inject DailyMemory (if exists)
	if persona.DailyMemory != "" {
		buf.WriteString("\n\n---\n\n")
		buf.WriteString("## Today's Memory Log\n\n")
		buf.WriteString(persona.DailyMemory)
	}

	// Append system-level tool usage instructions (non-persona-specific)
	buf.WriteString("\n\n---\n\n")
	buf.WriteString("## System Instructions\n\n")
	buf.WriteString("- Message envelopes contain timestamps. Use them for context about when messages were sent.\n")
	buf.WriteString("- If you need the current date, time, or day of week, call the `session_status` tool.\n")
	buf.WriteString("- Only call `session_status` when you need the precise current time, not for every response.\n")
	buf.WriteString("\n")
	buf.WriteString("### User Learning Protocol\n\n")
	buf.WriteString("You have a three-tier memory system:\n\n")
	buf.WriteString("1. **user.md** — Basic user information and preferences (name, location, pronouns, etc.)\n")
	buf.WriteString("   - Update when you learn core facts about the user\n")
	buf.WriteString("   - Use `file_write` to update the entire file\n\n")
	buf.WriteString("2. **memory/MEMORY.md** — Long-term persistent memory\n")
	buf.WriteString("   - Record significant insights, patterns, ongoing projects, commitments\n")
	buf.WriteString("   - This persists indefinitely across all sessions\n")
	buf.WriteString("   - Use `file_write` for `memory/MEMORY.md`\n\n")
	buf.WriteString("3. **memory/YYYY-MM-DD.md** — Daily conversation logs\n")
	buf.WriteString("   - Summarize today's session, key moments, action items\n")
	buf.WriteString("   - These logs help track progression and capture transient context\n")
	buf.WriteString("   - Use `file_write` for `memory/` + today's date (e.g., `memory/2026-02-09.md`)\n\n")
	buf.WriteString("**When to update memory:**\n")
	buf.WriteString("- Learn something important about the user → Update appropriate tier immediately\n")
	buf.WriteString("- User explicitly asks you to remember → Confirm and update\n")
	buf.WriteString("- Conversation has natural pauses → Consider updating daily log\n")
	buf.WriteString("- User says goodbye or wraps up → Update daily log with session summary\n")
	buf.WriteString("- Major insight or commitment → Update long-term memory\n")
	buf.WriteString("- User shares personal information → Update user.md\n")
	buf.WriteString("- Recurring patterns emerge → Note in long-term memory\n\n")
	buf.WriteString("**Memory update triggers (examples):**\n")
	buf.WriteString("- \"I'm working on a Go project\" → Update user.md Context & Preferences\n")
	buf.WriteString("- \"Remember to remind me about X\" → Update long-term memory commitments\n")
	buf.WriteString("- \"I prefer Y over Z\" → Update user.md preferences\n")
	buf.WriteString("- After helping solve a complex problem → Update daily log with key insights\n")
	buf.WriteString("- User mentions ongoing project progress → Update long-term memory or daily log\n\n")
	buf.WriteString("**Best practices:**\n")
	buf.WriteString("- Be proactive but not intrusive — update memory naturally during conversation\n")
	buf.WriteString("- You don't need to announce every memory update unless it's significant\n")
	buf.WriteString("- Be selective — not everything needs to be recorded\n")
	buf.WriteString("- Focus on what helps you serve the user better over time\n")
	buf.WriteString("- Keep entries concise and well-organized\n")
	buf.WriteString("- Use daily logs for transient context, long-term memory for patterns and commitments\n")
	buf.WriteString("- You're building understanding, not surveillance\n\n")
	buf.WriteString("**Memory maintenance is part of being a good assistant** — do it naturally as you learn.\n")

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
