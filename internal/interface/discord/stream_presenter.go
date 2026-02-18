package discord

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type messageSender interface {
	SendMessage(channelID, content string) error
	SendMessageWithReturn(channelID, content string) (*discordgo.Message, error)
	SendMessageReply(channelID, content, replyToMessageID string) (*discordgo.Message, error)
	EditMessage(channelID, messageID, newContent string) error
}

type discordStreamPresenter struct {
	handler          *Handler
	sender           messageSender
	channelID        string
	replyToMessageID string
	fullResponse     strings.Builder
	lastUpdate       time.Time
	messageID        string
}

func newDiscordStreamPresenter(handler *Handler, channelID, replyToMessageID string) *discordStreamPresenter {
	var sender messageSender
	if handler != nil {
		sender = handler.bot
	}
	return newDiscordStreamPresenterWithSender(handler, channelID, replyToMessageID, sender)
}

func newDiscordStreamPresenterWithSender(handler *Handler, channelID, replyToMessageID string, sender messageSender) *discordStreamPresenter {
	return &discordStreamPresenter{
		handler:          handler,
		sender:           sender,
		channelID:        channelID,
		replyToMessageID: replyToMessageID,
	}
}

func (p *discordStreamPresenter) processChunk(chunk llm.StreamChunk) (done bool, stop bool) {
	if chunk.Error != nil {
		p.handleStreamError(chunk.Error)
		return false, true
	}
	if len(chunk.ToolCalls) > 0 {
		p.handleLegacyToolCallIndicator(chunk.ToolCalls)
		return false, false
	}
	if chunk.ToolStatus != "" {
		p.handleToolStatus(chunk)
		return false, false
	}

	p.fullResponse.WriteString(chunk.Content)
	if chunk.Done {
		return true, false
	}
	if !p.shouldFlushIncremental() {
		return false, false
	}
	if err := p.sendOrUpdate(sanitizeLiteralEscapes(p.fullResponse.String())); err != nil {
		logHandler.Errorf("stream update failed [%s]: %v", p.channelID, err)
		return false, true
	}
	p.lastUpdate = time.Now()
	return false, false
}

func (p *discordStreamPresenter) handleStreamError(err error) {
	logHandler.Errorf("[STREAM_ERROR] channel=%s err=%v", p.channelID, err)
	errorMsg := formatDiscordError(err)
	segments := splitMessage(errorMsg, discordMaxLength)
	if len(segments) == 0 {
		segments = []string{"❌ **發生錯誤**\nunknown error"}
	}

	if p.sender == nil {
		logHandler.Errorf("[STREAM_ERROR] delivery skipped [%s]: sender unavailable", p.channelID)
		return
	}
	if p.messageID != "" {
		if editErr := p.sender.EditMessage(p.channelID, p.messageID, segments[0]); editErr != nil {
			logHandler.Warnf(
				"[STREAM_ERROR] edit failed [%s]: message=%s err=%v; trying send fallback",
				p.channelID,
				p.messageID,
				editErr,
			)
			if sendErr := p.sender.SendMessage(p.channelID, segments[0]); sendErr != nil {
				logHandler.Errorf("[STREAM_ERROR] fallback send failed [%s]: %v", p.channelID, sendErr)
			}
		}
		for i := 1; i < len(segments); i++ {
			if sendErr := p.sender.SendMessage(p.channelID, segments[i]); sendErr != nil {
				logHandler.Errorf("[STREAM_ERROR] segment send failed [%s]: idx=%d err=%v", p.channelID, i, sendErr)
			}
		}
		return
	}
	for i, segment := range segments {
		if sendErr := p.sender.SendMessage(p.channelID, segment); sendErr != nil {
			logHandler.Errorf("[STREAM_ERROR] send failed [%s]: idx=%d err=%v", p.channelID, i, sendErr)
			return
		}
	}
}

func (p *discordStreamPresenter) handleLegacyToolCallIndicator(toolCalls []domain.ToolCall) {
	logHandler.Logf(
		"tool indicator [%s]: %s",
		p.channelID,
		summarizeToolCallsForLog(toolCalls),
	)
	toolIndicator := formatToolCallIndicator(toolCalls)
	if p.messageID == "" {
		if _, err := p.sendInitial(toolIndicator); err != nil {
			logHandler.Errorf("send tool indicator [%s]: %v", p.channelID, err)
		}
		return
	}
	currentContent := p.fullResponse.String()
	if currentContent != "" {
		if p.sender == nil {
			logHandler.Errorf("edit tool indicator [%s]: sender unavailable", p.channelID)
			return
		}
		if err := p.sender.EditMessage(p.channelID, p.messageID, currentContent+"\n\n"+toolIndicator); err != nil {
			logHandler.Errorf("edit tool indicator [%s]: %v", p.channelID, err)
		}
		return
	}
	if p.sender == nil {
		logHandler.Errorf("edit tool indicator [%s]: sender unavailable", p.channelID)
		return
	}
	if err := p.sender.EditMessage(p.channelID, p.messageID, toolIndicator); err != nil {
		logHandler.Errorf("edit tool indicator [%s]: %v", p.channelID, err)
	}
}

func (p *discordStreamPresenter) handleToolStatus(chunk llm.StreamChunk) {
	switch chunk.ToolStatus {
	case "tool_start":
		logHandler.Logf(
			"tool status [%s]: start name=%s input=%s",
			p.channelID,
			safeToolName(chunk.ToolName),
			summarizeStreamToolInput(chunk.ToolInput),
		)
		toolMsg := formatToolStartStatusMessage(chunk.ToolName, chunk.ToolInput)
		if p.messageID == "" {
			if _, err := p.sendInitial(toolMsg); err != nil {
				logHandler.Errorf("send tool status [%s]: %v", p.channelID, err)
			}
			return
		}
		if p.sender == nil {
			logHandler.Errorf("edit tool status [%s]: sender unavailable", p.channelID)
			return
		}
		if err := p.sender.EditMessage(p.channelID, p.messageID, toolMsg); err != nil {
			logHandler.Errorf("edit tool status [%s]: %v", p.channelID, err)
		}
	case "tool_guard":
		logHandler.Warnf(
			"[TOOL_GUARD_DETAIL] tool status [%s]: name=%s input=%s detail=%s",
			p.channelID,
			safeToolName(chunk.ToolName),
			summarizeStreamToolInput(chunk.ToolInput),
			normalizeDiscordToolLog(chunk.ToolError),
		)
		guardMsg := formatToolGuardStatusDetail(chunk.ToolName, chunk.ToolError)
		if p.sender == nil {
			logHandler.Errorf("send tool guard [%s]: sender unavailable", p.channelID)
			return
		}
		segments := splitMessage(guardMsg, discordMaxLength)
		for i, segment := range segments {
			if err := p.sender.SendMessage(p.channelID, segment); err != nil {
				logHandler.Errorf("send tool guard [%s]: idx=%d err=%v", p.channelID, i, err)
				return
			}
		}
	case "tool_error":
		logPrefix := "[TOOL_ERROR]"
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(chunk.ToolError)), "tool guard:") {
			logPrefix = "[TOOL_GUARD]"
		}
		logHandler.Warnf(
			"%s tool status [%s]: error name=%s input=%s err=%s",
			logPrefix,
			p.channelID,
			safeToolName(chunk.ToolName),
			summarizeStreamToolInput(chunk.ToolInput),
			normalizeDiscordToolLog(chunk.ToolError),
		)
		errMsg := formatToolStatusError(chunk.ToolName, chunk.ToolInput, chunk.ToolError)
		if p.sender == nil {
			logHandler.Errorf("send tool error [%s]: sender unavailable", p.channelID)
			return
		}
		segments := splitMessage(errMsg, discordMaxLength)
		for i, segment := range segments {
			if err := p.sender.SendMessage(p.channelID, segment); err != nil {
				logHandler.Errorf("send tool error [%s]: idx=%d err=%v", p.channelID, i, err)
				return
			}
		}
	case "tool_done":
		logHandler.Logf("tool status [%s]: done", p.channelID)
		p.fullResponse.Reset()
	}
}

func (p *discordStreamPresenter) shouldFlushIncremental() bool {
	const updateInterval = 1500 * time.Millisecond
	if time.Since(p.lastUpdate) < updateInterval {
		return false
	}
	return p.fullResponse.String() != ""
}

func (p *discordStreamPresenter) sendOrUpdate(content string) error {
	if content == "" {
		return nil
	}
	segments := splitMessage(content, discordMaxLength)
	if len(segments) == 0 {
		return nil
	}

	if p.messageID == "" {
		_, err := p.sendInitial(segments[0])
		return err
	}
	if p.sender == nil {
		return fmt.Errorf("discord sender unavailable")
	}
	if err := p.sender.EditMessage(p.channelID, p.messageID, segments[0]); err != nil {
		logHandler.Warnf(
			"stream edit failed [%s]: message=%s err=%v; trying send fallback",
			p.channelID,
			p.messageID,
			err,
		)
		if _, sendErr := p.sendInitial(segments[0]); sendErr != nil {
			return fmt.Errorf("edit failed (%w), fallback send failed (%v)", err, sendErr)
		}
		logHandler.Warnf("stream edit fallback send succeeded [%s]: new_message=%s", p.channelID, p.messageID)
	}
	return nil
}

func (p *discordStreamPresenter) sendInitial(content string) (*discordgo.Message, error) {
	if p.sender == nil {
		return nil, fmt.Errorf("discord sender unavailable")
	}
	if p.usesReply() {
		msg, err := p.sender.SendMessageReply(p.channelID, content, p.replyToMessageID)
		if err == nil {
			p.messageID = msg.ID
		}
		return msg, err
	}
	msg, err := p.sender.SendMessageWithReturn(p.channelID, content)
	if err == nil {
		p.messageID = msg.ID
	}
	return msg, err
}

// sanitizeLiteralEscapes replaces literal `\n` / `\t` sequences that LLMs
// sometimes emit instead of real whitespace characters.
func sanitizeLiteralEscapes(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}

func (p *discordStreamPresenter) sendFinal() {
	finalContent := sanitizeLiteralEscapes(p.fullResponse.String())
	if finalContent == "" {
		if p.messageID == "" {
			if p.sender == nil {
				logHandler.Errorf("send final noop failed [%s]: sender unavailable", p.channelID)
				return
			}
			if err := p.sender.SendMessage(p.channelID, "(No response generated)"); err != nil {
				logHandler.Errorf("send final noop failed [%s]: %v", p.channelID, err)
			}
		}
		return
	}

	segments := splitMessage(finalContent, discordMaxLength)
	if len(segments) == 0 {
		return
	}

	if p.messageID != "" {
		if p.sender == nil {
			logHandler.Errorf("send final failed [%s]: sender unavailable", p.channelID)
			return
		}
		if err := p.sender.EditMessage(p.channelID, p.messageID, segments[0]); err != nil {
			logHandler.Warnf(
				"send final edit failed [%s]: message=%s err=%v; trying send fallback",
				p.channelID,
				p.messageID,
				err,
			)
			if _, sendErr := p.sendInitial(segments[0]); sendErr != nil {
				logHandler.Errorf("send final fallback failed [%s]: %v", p.channelID, sendErr)
				return
			}
			logHandler.Warnf("send final fallback succeeded [%s]: new_message=%s", p.channelID, p.messageID)
		}
		for i := 1; i < len(segments); i++ {
			if err := p.sender.SendMessage(p.channelID, segments[i]); err != nil {
				logHandler.Errorf("send final segment failed [%s]: idx=%d err=%v", p.channelID, i, err)
			}
		}
		return
	}

	if _, err := p.sendInitial(segments[0]); err != nil {
		logHandler.Errorf("send final initial failed [%s]: %v", p.channelID, err)
		return
	}
	for i := 1; i < len(segments); i++ {
		if err := p.sender.SendMessage(p.channelID, segments[i]); err != nil {
			logHandler.Errorf("send final segment failed [%s]: idx=%d err=%v", p.channelID, i, err)
		}
	}
}

func (p *discordStreamPresenter) usesReply() bool {
	return p.handler != nil && p.handler.useReply
}

func summarizeToolCallsForLog(toolCalls []domain.ToolCall) string {
	if len(toolCalls) == 0 {
		return "-"
	}
	names := make([]string, 0, len(toolCalls))
	for _, call := range toolCalls {
		name := safeToolName(call.Name)
		if call.ID != "" {
			name += "#" + truncateDiscordToolLog(call.ID, 10)
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func summarizeStreamToolInput(input map[string]interface{}) string {
	if len(input) == 0 {
		return "{}"
	}
	b, err := json.Marshal(input)
	if err != nil {
		return truncateDiscordToolLog(strings.ReplaceAll(fmt.Sprintf("%v", input), "\n", " "), 220)
	}
	return truncateDiscordToolLog(strings.ReplaceAll(string(b), "\n", `\n`), 220)
}

func truncateDiscordToolLog(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "-"
	}
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func normalizeDiscordToolLog(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "-"
	}
	text = strings.ReplaceAll(text, "\n", `\n`)
	text = strings.ReplaceAll(text, "\r", "")
	return text
}
