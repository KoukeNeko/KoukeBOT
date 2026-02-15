package discord

import (
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type discordStreamPresenter struct {
	handler          *Handler
	channelID        string
	replyToMessageID string
	fullResponse     strings.Builder
	lastUpdate       time.Time
	messageID        string
}

func newDiscordStreamPresenter(handler *Handler, channelID, replyToMessageID string) *discordStreamPresenter {
	return &discordStreamPresenter{
		handler:          handler,
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
	if err := p.sendOrUpdate(p.fullResponse.String()); err != nil {
		logHandler.Errorf("stream update failed [%s]: %v", p.channelID, err)
		return false, true
	}
	p.lastUpdate = time.Now()
	return false, false
}

func (p *discordStreamPresenter) handleStreamError(err error) {
	logHandler.Errorf("stream error [%s]: %v", p.channelID, err)
	errorMsg := formatDiscordError(err)
	if p.messageID != "" {
		_ = p.handler.bot.EditMessage(p.channelID, p.messageID, errorMsg)
		return
	}
	_ = p.handler.bot.SendMessage(p.channelID, errorMsg)
}

func (p *discordStreamPresenter) handleLegacyToolCallIndicator(toolCalls []domain.ToolCall) {
	toolIndicator := formatToolCallIndicator(toolCalls)
	if p.messageID == "" {
		if _, err := p.sendInitial(toolIndicator); err != nil {
			logHandler.Errorf("send tool indicator [%s]: %v", p.channelID, err)
		}
		return
	}
	currentContent := p.fullResponse.String()
	if currentContent != "" {
		_ = p.handler.bot.EditMessage(p.channelID, p.messageID, currentContent+"\n\n"+toolIndicator)
		return
	}
	_ = p.handler.bot.EditMessage(p.channelID, p.messageID, toolIndicator)
}

func (p *discordStreamPresenter) handleToolStatus(chunk llm.StreamChunk) {
	switch chunk.ToolStatus {
	case "tool_start":
		toolMsg := formatToolStartStatusMessage(chunk.ToolName, chunk.ToolInput)
		if p.messageID == "" {
			if _, err := p.sendInitial(toolMsg); err != nil {
				logHandler.Errorf("send tool status [%s]: %v", p.channelID, err)
			}
			return
		}
		_ = p.handler.bot.EditMessage(p.channelID, p.messageID, toolMsg)
	case "tool_error":
		errMsg := formatToolStatusError(chunk.ToolName, chunk.ToolInput, chunk.ToolError)
		if err := p.handler.bot.SendMessage(p.channelID, errMsg); err != nil {
			logHandler.Errorf("send tool error [%s]: %v", p.channelID, err)
		}
	case "tool_done":
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
	return p.handler.bot.EditMessage(p.channelID, p.messageID, segments[0])
}

func (p *discordStreamPresenter) sendInitial(content string) (*discordgo.Message, error) {
	if p.handler.useReply {
		msg, err := p.handler.bot.SendMessageReply(p.channelID, content, p.replyToMessageID)
		if err == nil {
			p.messageID = msg.ID
		}
		return msg, err
	}
	msg, err := p.handler.bot.SendMessageWithReturn(p.channelID, content)
	if err == nil {
		p.messageID = msg.ID
	}
	return msg, err
}

func (p *discordStreamPresenter) sendFinal() {
	finalContent := p.fullResponse.String()
	if finalContent == "" {
		if p.messageID == "" {
			_ = p.handler.bot.SendMessage(p.channelID, "(No response generated)")
		}
		return
	}

	segments := splitMessage(finalContent, discordMaxLength)
	if len(segments) == 0 {
		return
	}

	if p.messageID != "" {
		_ = p.handler.bot.EditMessage(p.channelID, p.messageID, segments[0])
		for i := 1; i < len(segments); i++ {
			_ = p.handler.bot.SendMessage(p.channelID, segments[i])
		}
		return
	}

	if p.handler.useReply {
		msg, err := p.handler.bot.SendMessageReply(p.channelID, segments[0], p.replyToMessageID)
		if err == nil {
			p.messageID = msg.ID
		}
		for i := 1; i < len(segments); i++ {
			_ = p.handler.bot.SendMessage(p.channelID, segments[i])
		}
		return
	}

	for _, segment := range segments {
		_ = p.handler.bot.SendMessage(p.channelID, segment)
	}
}
