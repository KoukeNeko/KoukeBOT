package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// executeMemoryFlush performs a silent turn: asks the agent to save important
// memories via file tools, then compacts history. Invisible to the user.
func (s *ChatService) executeMemoryFlush(ctx context.Context, conv *domain.Conversation, persona *domain.PersonaModule, personaID string) error {
	logChat.Log("executing pre-compaction memory flush...")

	flushPrompt := fmt.Sprintf(
		"SYSTEM: Context window approaching limit. Review this conversation and save important information:\n"+
			"- User facts → user.md (file_write)\n"+
			"- Relationship context, ongoing projects → memory/MEMORY.md (file_write)\n"+
			"- Today's session notes → memory/%s.md (file_append)\n\n"+
			"After updating files (or if nothing needs storing), reply with exactly: NO_REPLY\n"+
			"This message will not be shown to the user.",
		time.Now().Format("2006-01-02"),
	)

	// Temporarily append flush prompt for context assembly
	conv.AppendUserMessage(flushPrompt)
	assembled, err := s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return fmt.Errorf("assemble flush context: %w", err)
	}

	// Use streaming to support tool calls during flush
	streamCh, err := s.provider.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: assembled.SystemPrompt,
		Messages:     assembled.Messages,
		Generation:   assembled.Generation,
		ToolMode:     llm.ToolModeNormal,
	})
	if err != nil {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
		return fmt.Errorf("flush LLM call: %w", err)
	}

	// Consume stream, handle tool calls (up to 3 rounds)
	const maxFlushRounds = 3
	var flushResponse string
	var flushTokens int
	for round := 0; round <= maxFlushRounds; round++ {
		var pendingToolCalls []domain.ToolCall
		flushResponse = ""

		for chunk := range streamCh {
			if chunk.TokensUsed > 0 {
				flushTokens += chunk.TokensUsed
			}
			if len(chunk.ToolCalls) > 0 {
				pendingToolCalls = chunk.ToolCalls
				continue
			}
			if chunk.Error != nil {
				conv.Messages = conv.Messages[:len(conv.Messages)-1]
				return fmt.Errorf("flush stream error: %w", chunk.Error)
			}
			flushResponse += chunk.Content
		}

		if len(pendingToolCalls) == 0 {
			break
		}
		if round == maxFlushRounds {
			break
		}

		// Execute tools (file_write, file_append)
		names := make([]string, len(pendingToolCalls))
		for i, c := range pendingToolCalls {
			names[i] = c.Name
		}
		logChat.Logf("flush tool calls: %s", strings.Join(names, ", "))

		toolResults := s.executeTools(ctx, pendingToolCalls, personaID)
		toolResultsText := formatToolResults(pendingToolCalls, toolResults)

		// Append tool exchange to conversation for next round
		conv.AppendAssistantMessage("[Memory flush tool calls]")
		conv.Messages = append(conv.Messages, domain.Message{
			Role:    domain.RoleUser,
			Content: toolResultsText,
		})

		// Continue conversation with tool results
		assembled, err = s.assembler.Assemble(persona, conv, s.provider.ModelInfo())
		if err != nil {
			logChat.Warnf("flush assemble failed at round %d: %v", round, err)
			break
		}
		streamCh, err = s.provider.StreamChat(ctx, llm.ChatRequest{
			SystemPrompt: assembled.SystemPrompt,
			Messages:     assembled.Messages,
			Generation:   assembled.Generation,
			ToolMode:     llm.ToolModeNormal,
		})
		if err != nil {
			logChat.Warnf("flush stream continuation failed at round %d: %v", round, err)
			break
		}
	}

	// Remove all flush-related messages from history (invisible to user)
	// Find and remove everything after the original conversation
	// The flush prompt was appended, so we need to remove it and any tool exchanges
	msgCount := len(conv.Messages)
	// Walk backwards to find the flush prompt (first message we added)
	for i := msgCount - 1; i >= 0; i-- {
		if conv.Messages[i].Content == flushPrompt {
			conv.Messages = conv.Messages[:i]
			break
		}
	}

	// Check response
	trimmed := strings.TrimSpace(flushResponse)
	if trimmed == "NO_REPLY" || trimmed == "[NO_REPLY]" {
		logChat.Log("flush complete: no memories to store")
	} else {
		logChat.Logf("flush complete: agent responded (%d chars)", len(trimmed))
	}

	// Attempt LLM-driven summarization of messages that will be removed.
	keepRecent := s.keepRecentUserTurnsSnapshot()
	cutoff := findTurnCutoffByUserTurns(conv.Messages, keepRecent)

	var preBuiltDigest *domain.ConversationDigest
	if cutoff > 0 {
		removed := conv.Messages[:cutoff]
		llmDigest, err := SummarizeConversationBlock(ctx, s.provider, SummarizeRequest{
			Messages: removed,
			Persona:  persona,
		})
		if err != nil {
			logChat.Warnf("llm summarization failed, falling back to rule-based: %v", err)
		} else {
			preBuiltDigest = &llmDigest
		}
	}

	// Compact history (uses LLM digest if available, otherwise rule-based fallback)
	s.contextManager.CompactHistoryWithDigest(conv, persona, preBuiltDigest)
	conv.AddUsageTokens(flushTokens)
	if flushTokens > 0 {
		logChat.Logf("flush tokens: +%d (usage_total: %d, context_estimate: %d)", flushTokens, conv.UsageTokensTotal, conv.CumulativeTokens)
	}

	// Persist compacted conversation
	if err := s.store.Save(ctx, conv); err != nil {
		return fmt.Errorf("save after flush: %w", err)
	}

	logChat.Log("memory flush and compaction done")
	return nil
}
