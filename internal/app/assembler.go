package app

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/util"
)

const estimatedCharsPerToken = 4

// AssembledContext is the final prompt payload ready to send to a Provider.
type AssembledContext struct {
	SystemPrompt string
	Messages     []domain.Message
	Generation   domain.GenerationConfig
}

// ContextBudgetManager controls prompt budget allocation between digest and recent history.
type ContextBudgetManager struct {
	ResponseReserveTokens int     // reserved for model output + safety
	MaxRecentMessages     int     // hard cap for recent raw turns
	MaxDigests            int     // max digest blocks to include
	DigestBudgetRatio     float64 // % of history budget reserved for digest blocks
	PersonaLockEveryTurns int     // inject a short persona lock reminder every N user turns
}

func NewContextBudgetManager() *ContextBudgetManager {
	return &ContextBudgetManager{
		ResponseReserveTokens: 4096,
		MaxRecentMessages:     40,
		MaxDigests:            4,
		DigestBudgetRatio:     0.30,
		PersonaLockEveryTurns: 8,
	}
}

func (bm *ContextBudgetManager) historyBudget(systemPrompt string, caps llm.ModelCapabilities, generation domain.GenerationConfig) int {
	window := caps.ContextWindow
	if window <= 0 {
		window = 800_000
	}

	reserve := bm.ResponseReserveTokens
	if generation.MaxTokens > 0 {
		reserve = generation.MaxTokens + 1024 // response + safety margin
	}

	systemTokens := estimateTokens(systemPrompt)
	budget := window - reserve - systemTokens
	if budget < 1200 {
		budget = 1200
	}
	return budget
}

// ContextAssembler builds the final prompt from a PersonaModule + Conversation,
// choosing the topology based on model capabilities.
type ContextAssembler struct {
	budget *ContextBudgetManager
}

func NewContextAssembler() *ContextAssembler {
	return &ContextAssembler{budget: NewContextBudgetManager()}
}

// Assemble builds the complete context for an LLM call.
// For thinking models: static system prompt + relevant digests + budgeted recent history.
// For legacy models: same system + anchors as few-shot message pairs + recent N.
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
		messages = a.assembleThinkingModel(persona, conv, systemPrompt, caps, persona.Config.Generation)
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
	buf.WriteString("### Memory System\n\n")
	buf.WriteString("You maintain three tiers of memory:\n")
	buf.WriteString("- **user.md**: User profile (name, preferences, personality, habits)\n")
	buf.WriteString("- **MEMORY.md**: Long-term context (projects, relationship dynamics, shared history)\n")
	buf.WriteString("- **Daily logs**: Session notes (memory/YYYY-MM-DD.md)\n\n")
	buf.WriteString("Proactively use `file_write` to update user.md or MEMORY.md when learning important information about the user.\n")
	buf.WriteString("Use `file_append` to add notes to today's daily log throughout the conversation.\n")
	buf.WriteString("When context approaches limits, you'll be prompted to consolidate memories before compaction.\n")

	return buf.String(), nil
}

func (a *ContextAssembler) assembleThinkingModel(
	persona *domain.PersonaModule,
	conv *domain.Conversation,
	systemPrompt string,
	caps llm.ModelCapabilities,
	generation domain.GenerationConfig,
) []domain.Message {
	budgetTokens := a.budget.historyBudget(systemPrompt, caps, generation)

	recentBudget := int(float64(budgetTokens) * (1.0 - a.budget.DigestBudgetRatio))
	digestBudget := budgetTokens - recentBudget

	currentUserContent := ""
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if conv.Messages[i].Role == domain.RoleUser {
			currentUserContent = conv.Messages[i].Content
			break
		}
	}

	var out []domain.Message
	digestMsg := a.buildDigestMessage(a.selectRelevantDigests(conv.ContextDigests, currentUserContent), digestBudget)
	if digestMsg != "" {
		out = append(out, domain.Message{
			Role:    domain.RoleUser,
			Content: digestMsg,
		})
	}
	if a.shouldInjectPersonaLock(conv) {
		out = append(out, domain.Message{
			Role:    domain.RoleUser,
			Content: a.buildPersonaLockMessage(persona),
		})
	}

	recent := a.selectRecentMessages(conv.Messages, recentBudget, a.budget.MaxRecentMessages)
	out = append(out, recent...)

	if len(out) == 0 {
		return a.selectRecentMessages(conv.Messages, 1200, 8)
	}
	return out
}

func (a *ContextAssembler) shouldInjectPersonaLock(conv *domain.Conversation) bool {
	n := a.budget.PersonaLockEveryTurns
	if n <= 0 {
		return false
	}

	userTurns := 0
	for _, msg := range conv.Messages {
		if msg.Role == domain.RoleUser {
			userTurns++
		}
	}
	return userTurns >= n && userTurns%n == 0
}

func (a *ContextAssembler) buildPersonaLockMessage(persona *domain.PersonaModule) string {
	name := strings.TrimSpace(persona.Config.Meta.Name)
	if name == "" {
		name = strings.TrimSpace(persona.Config.Meta.ID)
	}
	if name == "" {
		name = "the configured persona"
	}

	// Use one anchor snippet as lightweight style reinforcement.
	styleRef := ""
	if len(persona.Anchors) > 0 {
		styleRef = truncate(strings.TrimSpace(persona.Anchors[0].BotResponse), 180)
	}

	var sb strings.Builder
	sb.WriteString("[Persona lock]\n")
	sb.WriteString("Stay fully in character as ")
	sb.WriteString(name)
	sb.WriteString(". Keep your established tone, quirks, and relationship style consistent.")
	if styleRef != "" {
		sb.WriteString("\nStyle reference: ")
		sb.WriteString(styleRef)
	}
	return sb.String()
}

func (a *ContextAssembler) selectRecentMessages(messages []domain.Message, budgetTokens, maxMessages int) []domain.Message {
	if len(messages) == 0 {
		return nil
	}

	if maxMessages <= 0 {
		maxMessages = 20
	}

	selected := make([]domain.Message, 0, maxMessages)
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if len(selected) >= maxMessages {
			break
		}

		msg := messages[i]
		compact := domain.Message{Role: msg.Role, Content: msg.Content}
		if len(selected) >= 4 && len(compact.Content) > 4000 {
			compact.Content = compact.Content[:4000] + "\n...[history truncated for token budget]"
		}

		cost := estimateTokens(compact.Content)
		if used+cost > budgetTokens {
			if len(selected) == 0 {
				// Always keep at least the latest message.
				selected = append(selected, compact)
			}
			break
		}

		used += cost
		selected = append(selected, compact)
	}

	// reverse
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

func (a *ContextAssembler) selectRelevantDigests(digests []domain.ConversationDigest, currentMessage string) []domain.ConversationDigest {
	if len(digests) == 0 {
		return nil
	}

	queryKeywords := extractTopKeywords(currentMessage, 20)
	keywordSet := make(map[string]bool, len(queryKeywords))
	for _, k := range queryKeywords {
		keywordSet[strings.ToLower(k)] = true
	}

	type scored struct {
		digest domain.ConversationDigest
		score  float64
	}
	items := make([]scored, 0, len(digests))
	for _, d := range digests {
		overlap := 0.0
		for _, k := range d.Keywords {
			if keywordSet[strings.ToLower(k)] {
				overlap += 1.0
			}
		}
		recencyBoost := float64(d.CreatedAt.Unix()) / 1e10
		score := overlap*5.0 + recencyBoost
		items = append(items, scored{digest: d, score: score})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].digest.CreatedAt.After(items[j].digest.CreatedAt)
		}
		return items[i].score > items[j].score
	})

	limit := a.budget.MaxDigests
	if limit <= 0 {
		limit = 3
	}
	if len(items) < limit {
		limit = len(items)
	}

	result := make([]domain.ConversationDigest, 0, limit)
	for i := 0; i < limit; i++ {
		// If query is empty, keep only the latest digest by default.
		if len(queryKeywords) == 0 && i > 0 {
			break
		}
		result = append(result, items[i].digest)
	}

	// Keep chronological order for readability in prompt.
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (a *ContextAssembler) buildDigestMessage(digests []domain.ConversationDigest, budgetTokens int) string {
	if len(digests) == 0 || budgetTokens <= 0 {
		return ""
	}

	maxChars := budgetTokens * estimatedCharsPerToken
	if maxChars < 400 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Relevant conversation digests]\n")
	for i, d := range digests {
		sb.WriteString(fmt.Sprintf("\nDigest %d (%s)\n", i+1, d.CreatedAt.Format(time.RFC3339)))
		sb.WriteString("Summary:\n")
		sb.WriteString(truncate(d.Summary, 1200))
		sb.WriteString("\n")
		if len(d.Facts) > 0 {
			sb.WriteString("Facts: " + strings.Join(d.Facts, " | ") + "\n")
		}
		if len(d.Preferences) > 0 {
			sb.WriteString("Preferences: " + strings.Join(d.Preferences, " | ") + "\n")
		}
		if len(d.OpenItems) > 0 {
			sb.WriteString("Open items: " + strings.Join(d.OpenItems, " | ") + "\n")
		}
		if len(d.Decisions) > 0 {
			sb.WriteString("Decisions: " + strings.Join(d.Decisions, " | ") + "\n")
		}
	}

	msg := sb.String()
	if len(msg) > maxChars {
		msg = msg[:maxChars] + "\n...[digest truncated for token budget]"
	}
	return msg
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

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	tokens := len(s) / estimatedCharsPerToken
	if tokens <= 0 {
		return 1
	}
	return tokens
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
