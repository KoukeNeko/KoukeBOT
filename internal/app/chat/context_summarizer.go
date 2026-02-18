package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// SummarizeRequest is the input for LLM-based conversation summarization.
type SummarizeRequest struct {
	Messages []domain.Message
	Persona  *domain.PersonaModule
}

// llmDigestResponse is the JSON schema the LLM produces.
type llmDigestResponse struct {
	Summary       string   `json:"summary"`
	Facts         []string `json:"facts"`
	Preferences   []string `json:"preferences"`
	OpenItems     []string `json:"open_items"`
	Decisions     []string `json:"decisions"`
	Keywords      []string `json:"keywords"`
	Attitude      string   `json:"attitude"`
	EmotionalTone string   `json:"emotional_tone"`
	StyleQuotes   []string `json:"style_quotes"`
}

const summarizeSystemPrompt = `You are a conversation summarizer. Your job is to distill a conversation block into a structured JSON digest that preserves:
1. Key topics, outcomes, and decisions
2. User facts and preferences discovered
3. Unresolved tasks or open items
4. The dominant conversational style and attitude

Output ONLY valid JSON matching this schema (no markdown fences, no extra text):
{
  "summary": "2-3 sentence summary of key topics and outcomes",
  "facts": ["factual statements about the user (max 8)"],
  "preferences": ["user preferences discovered (max 8)"],
  "open_items": ["unresolved tasks or questions (max 10)"],
  "decisions": ["decisions made in this block (max 10)"],
  "keywords": ["important keywords for retrieval (max 16)"],
  "attitude": "dominant attitude: critical | directive | playful | formal | (empty string if unclear)",
  "emotional_tone": "dominant tone: cold | irritated | composed | (empty string if unclear)",
  "style_quotes": ["1-3 representative quotes showing persona communication style"]
}

Rules:
- Keep the summary concise and information-dense.
- Facts/preferences must be actual statements from the user, not inferred.
- Style quotes should be exact excerpts from assistant messages that best represent the persona's tone.
- If a field has no relevant data, use an empty array [] or empty string "".
- Output raw JSON only. No markdown code fences. No explanation.`

const summarizeMaxInputChars = 24000 // Limit input to ~6000 tokens to keep cost low
const summarizeTimeoutSec = 30

// SummarizeConversationBlock uses LLM to produce a structured digest from removed messages.
// Falls back to rule-based buildDigest on any error.
func SummarizeConversationBlock(
	ctx context.Context,
	provider llm.Provider,
	req SummarizeRequest,
) (domain.ConversationDigest, error) {
	if len(req.Messages) == 0 {
		return domain.ConversationDigest{}, fmt.Errorf("no messages to summarize")
	}

	formatted := formatMessagesForSummary(req.Messages)
	if len(formatted) > summarizeMaxInputChars {
		formatted = formatted[len(formatted)-summarizeMaxInputChars:]
	}

	userPrompt := "Summarize this conversation block:\n\n" + formatted

	summarizeCtx, cancel := context.WithTimeout(ctx, summarizeTimeoutSec*time.Second)
	defer cancel()

	resp, err := provider.Chat(summarizeCtx, llm.ChatRequest{
		SystemPrompt: summarizeSystemPrompt,
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: userPrompt},
		},
		Generation: domain.GenerationConfig{
			Temperature: 0.2,
			TopP:        0.9,
		},
		ToolMode: llm.ToolModeDisabled,
	})
	if err != nil {
		return domain.ConversationDigest{}, fmt.Errorf("llm summarize call: %w", err)
	}

	digest, err := parseLLMDigestResponse(resp.Content)
	if err != nil {
		return domain.ConversationDigest{}, fmt.Errorf("parse llm digest: %w", err)
	}

	logCtx.Logf(
		"llm digest: summary_len=%d facts=%d prefs=%d open=%d decisions=%d keywords=%d attitude=%q tone=%q quotes=%d",
		len(digest.Summary), len(digest.Facts), len(digest.Preferences),
		len(digest.OpenItems), len(digest.Decisions), len(digest.Keywords),
		digest.Attitude, digest.EmotionalTone, len(digest.StyleQuotes),
	)
	return digest, nil
}

// summarizePerMessageMaxChars caps individual messages to avoid one huge message dominating the summary input.
const summarizePerMessageMaxChars = 1200

// formatMessagesForSummary formats messages into readable text for the LLM.
// Skips internal tool result messages (zero summarization value, high token cost).
func formatMessagesForSummary(messages []domain.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		if isInternalToolHistoryMessage(msg.Content) {
			continue
		}
		text := humanizeConversationTextForDigest(msg.Content)
		if text == "" {
			continue
		}
		if len(text) > summarizePerMessageMaxChars {
			text = text[:summarizePerMessageMaxChars] + "..."
		}
		role := "Assistant"
		if msg.Role == domain.RoleUser {
			role = "User"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n\n", role, text))
	}
	return sb.String()
}

// parseLLMDigestResponse parses the LLM JSON response into a ConversationDigest.
func parseLLMDigestResponse(raw string) (domain.ConversationDigest, error) {
	cleaned := cleanJSONResponse(raw)
	if cleaned == "" {
		return domain.ConversationDigest{}, fmt.Errorf("empty response from LLM")
	}

	var parsed llmDigestResponse
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return domain.ConversationDigest{}, fmt.Errorf("json unmarshal: %w (raw: %s)", err, truncate(cleaned, 200))
	}

	if parsed.Summary == "" {
		return domain.ConversationDigest{}, fmt.Errorf("LLM produced empty summary")
	}

	// Enforce field limits matching rule-based digest constraints
	parsed.Facts = capSlice(parsed.Facts, 8)
	parsed.Preferences = capSlice(parsed.Preferences, 8)
	parsed.OpenItems = capSlice(parsed.OpenItems, 10)
	parsed.Decisions = capSlice(parsed.Decisions, 10)
	parsed.Keywords = capSlice(parsed.Keywords, 16)
	parsed.StyleQuotes = capSlice(parsed.StyleQuotes, 3)

	// Validate attitude and emotional tone against known values
	parsed.Attitude = validateEnum(parsed.Attitude, "critical", "directive", "playful", "formal")
	parsed.EmotionalTone = validateEnum(parsed.EmotionalTone, "cold", "irritated", "composed")

	return domain.ConversationDigest{
		ID:            fmt.Sprintf("digest-%d", time.Now().UnixNano()),
		Summary:       parsed.Summary,
		Facts:         parsed.Facts,
		Preferences:   parsed.Preferences,
		OpenItems:     parsed.OpenItems,
		Decisions:     parsed.Decisions,
		Keywords:      parsed.Keywords,
		Attitude:      parsed.Attitude,
		EmotionalTone: parsed.EmotionalTone,
		StyleQuotes:   parsed.StyleQuotes,
		CreatedAt:     time.Now(),
	}, nil
}

// cleanJSONResponse strips markdown fences and whitespace from LLM output.
func cleanJSONResponse(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip markdown code fences
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func capSlice(s []string, max int) []string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func validateEnum(value string, allowed ...string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}
