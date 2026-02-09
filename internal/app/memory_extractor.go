package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logMemory = logger.New("memory", logger.Green)

// MemoryExtractor analyzes conversation exchanges in the background
// and updates the three-tier memory system (user.md, MEMORY.md, daily log) automatically.
// It uses a separate, cheap LLM (e.g., gemini-2.0-flash) so the user sees no delay.
type MemoryExtractor struct {
	provider    llm.Provider // Cheap, fast LLM provider (no tools registered)
	personasDir string       // Base dir: "configs/personas"
	mu          sync.Mutex   // Serialize extractions to prevent file write races
}

// NewMemoryExtractor creates a memory extractor with a dedicated cheap LLM provider.
func NewMemoryExtractor(provider llm.Provider, personasDir string) *MemoryExtractor {
	return &MemoryExtractor{
		provider:    provider,
		personasDir: personasDir,
	}
}

// extractionResult is the JSON schema the extraction LLM returns.
type extractionResult struct {
	ShouldUpdate   bool    `json:"should_update"`
	UserProfile    *string `json:"user_profile"`     // nil = no change, string = complete replacement
	LongTermMemory *string `json:"long_term_memory"` // nil = no change, string = complete replacement
	DailyLogEntry  *string `json:"daily_log_entry"`  // nil = no entry, string = text to append
}

// Process analyzes the latest exchange and updates memory files if warranted.
// Designed to be called as a goroutine: go extractor.Process(...)
func (me *MemoryExtractor) Process(
	ctx context.Context,
	personaDirName string,
	userMessage string,
	assistantResponse string,
	persona *domain.PersonaModule,
) {
	me.mu.Lock()
	defer me.mu.Unlock()

	logMemory.Logf("extracting from exchange (%d + %d chars)...", len(userMessage), len(assistantResponse))

	// Build extraction prompt with current memory context
	prompt := me.buildExtractionPrompt(userMessage, assistantResponse, persona)

	// Call the cheap LLM (non-streaming, fast)
	resp, err := me.provider.Chat(ctx, llm.ChatRequest{
		SystemPrompt: extractionSystemPrompt,
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: prompt},
		},
		Generation: domain.GenerationConfig{
			Temperature: 0.1, // Low temperature for deterministic extraction
			MaxTokens:   4096,
		},
	})
	if err != nil {
		logMemory.Warnf("extraction failed: %v", err)
		return
	}

	// Parse JSON response (strip markdown code fences if present)
	jsonStr := cleanJSONResponse(resp.Content)

	var result extractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logMemory.Warnf("JSON parse failed: %v (raw: %s)", err, truncateForLog(resp.Content, 200))
		return
	}

	if !result.ShouldUpdate {
		logMemory.Logf("no update needed")
		return
	}

	// Apply updates to disk and in-memory persona
	personaDir := filepath.Join(me.personasDir, personaDirName)

	if result.UserProfile != nil {
		if err := writeFile(filepath.Join(personaDir, "user.md"), *result.UserProfile); err != nil {
			logMemory.Warnf("user.md write failed: %v", err)
		} else {
			persona.UserProfile = *result.UserProfile
			logMemory.Logf("updated user.md (%d chars)", len(*result.UserProfile))
		}
	}

	if result.LongTermMemory != nil {
		memoryDir := filepath.Join(personaDir, "memory")
		_ = os.MkdirAll(memoryDir, 0755)
		if err := writeFile(filepath.Join(memoryDir, "MEMORY.md"), *result.LongTermMemory); err != nil {
			logMemory.Warnf("MEMORY.md write failed: %v", err)
		} else {
			persona.LongTermMemory = *result.LongTermMemory
			logMemory.Logf("updated MEMORY.md (%d chars)", len(*result.LongTermMemory))
		}
	}

	if result.DailyLogEntry != nil {
		memoryDir := filepath.Join(personaDir, "memory")
		_ = os.MkdirAll(memoryDir, 0755)
		now := time.Now()
		filename := fmt.Sprintf("%d-%02d-%02d.md", now.Year(), now.Month(), now.Day())
		if err := appendFile(filepath.Join(memoryDir, filename), *result.DailyLogEntry+"\n"); err != nil {
			logMemory.Warnf("daily log append failed: %v", err)
		} else {
			if persona.DailyMemory == "" {
				persona.DailyMemory = *result.DailyLogEntry
			} else {
				persona.DailyMemory += "\n" + *result.DailyLogEntry
			}
			logMemory.Logf("appended daily log (%d chars)", len(*result.DailyLogEntry))
		}
	}
}

func (me *MemoryExtractor) buildExtractionPrompt(
	userMessage, assistantResponse string,
	persona *domain.PersonaModule,
) string {
	var sb strings.Builder

	sb.WriteString("## Current User Profile (user.md)\n\n")
	if persona.UserProfile != "" {
		sb.WriteString(persona.UserProfile)
	} else {
		sb.WriteString("(empty)")
	}

	sb.WriteString("\n\n## Current Long-Term Memory (MEMORY.md)\n\n")
	if persona.LongTermMemory != "" {
		sb.WriteString(persona.LongTermMemory)
	} else {
		sb.WriteString("(empty)")
	}

	sb.WriteString("\n\n## Today's Daily Log\n\n")
	if persona.DailyMemory != "" {
		sb.WriteString(persona.DailyMemory)
	} else {
		sb.WriteString("(no entries yet today)")
	}

	sb.WriteString("\n\n## Latest Exchange\n\n")
	sb.WriteString("**User:** ")
	sb.WriteString(userMessage)
	sb.WriteString("\n\n**Assistant:** ")
	sb.WriteString(assistantResponse)

	sb.WriteString("\n\n---\n\nAnalyze this exchange and return your JSON response.")

	return sb.String()
}

// writeFile writes content to a file, creating it if needed.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// appendFile appends content to a file, creating it if needed.
func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// cleanJSONResponse strips markdown code fences from LLM output.
func cleanJSONResponse(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// truncateForLog truncates a string for log output.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

const extractionSystemPrompt = `You are a memory extraction system for a conversational AI assistant. Your job is to analyze a conversation exchange and determine what should be saved to the user's memory files.

You have access to three memory tiers:
1. **user.md** — User profile: name, location, preferences, profession, personality, hobbies, habits, likes/dislikes, relationships. Update when you learn ANY new fact about the user.
2. **MEMORY.md** — Long-term memory: ongoing projects, relationship dynamics, important decisions, recurring topics, inside jokes, shared context. Update for anything that helps the assistant understand the user better over time.
3. **Daily log** — Session journal: what was discussed, topics, mood, interesting moments, questions asked. Append brief entries to maintain continuity.

RULES:
- Be proactive about saving memories. Even casual chat reveals useful context (mood, interests, opinions, what they're doing).
- Update user.md when you learn new facts about the user OR when existing facts need refinement.
- Update MEMORY.md for relationship-building context: shared jokes, recurring themes, evolving topics, project progress.
- Use daily log freely — it's cheap storage. Log conversation topics, user mood/energy, notable exchanges. This helps with "what were we talking about?" continuity.
- When updating user.md or MEMORY.md, return the COMPLETE updated file content (preserve all existing information, only add/refine).
- When adding a daily log entry, return ONLY the new entry to append (not the full file). Prefix with a timestamp like "- [HH:MM]".
- Do NOT fabricate information. Only record what is explicitly stated or clearly implied.
- Set should_update to false ONLY when the exchange contains absolutely nothing worth remembering (e.g., a single "ok" or "thanks").

Respond with ONLY a JSON object (no markdown fences, no explanation):
{"should_update": false, "user_profile": null, "long_term_memory": null, "daily_log_entry": null}`
