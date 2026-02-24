package discord

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// isMentioned checks if the bot was @mentioned in the message.
func isMentioned(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	for _, user := range m.Mentions {
		if user.ID == s.State.User.ID {
			return true
		}
	}
	return false
}

// isDM checks if the message is a direct message.
func isDM(m *discordgo.MessageCreate) bool {
	return m.GuildID == ""
}

// isReplyToBot checks if the message is a Discord reply targeting this bot.
func isReplyToBot(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if s == nil || s.State == nil || s.State.User == nil || m == nil || m.MessageReference == nil {
		return false
	}
	if strings.TrimSpace(m.MessageReference.MessageID) == "" {
		return false
	}

	botID := strings.TrimSpace(s.State.User.ID)
	if botID == "" {
		return false
	}

	// Discord often includes the referenced message in gateway payloads.
	// Use it first to avoid unnecessary API calls.
	if m.ReferencedMessage != nil && m.ReferencedMessage.Author != nil {
		return strings.TrimSpace(m.ReferencedMessage.Author.ID) == botID
	}

	refChannelID := strings.TrimSpace(m.ChannelID)
	if strings.TrimSpace(m.MessageReference.ChannelID) != "" {
		refChannelID = strings.TrimSpace(m.MessageReference.ChannelID)
	}
	if refChannelID == "" {
		return false
	}

	refMsg, err := s.ChannelMessage(refChannelID, m.MessageReference.MessageID)
	if err != nil {
		logHandler.Warnf("reply-target fetch failed for trigger [%s/%s]: %v", refChannelID, m.MessageReference.MessageID, err)
		return false
	}
	return refMsg != nil && refMsg.Author != nil && strings.TrimSpace(refMsg.Author.ID) == botID
}

// cleanMention strips only leading bot @mentions and preserves all other tags.
func cleanMention(s *discordgo.Session, m *discordgo.MessageCreate, stripLeadingBotMentionPrefix bool) string {
	if m == nil {
		return ""
	}
	content := strings.TrimSpace(m.Content)
	botID := ""
	if s != nil && s.State != nil && s.State.User != nil {
		botID = s.State.User.ID
	}
	if stripLeadingBotMentionPrefix {
		content = trimLeadingBotMentions(content, botID)
	}
	if strings.TrimSpace(content) != "" {
		return content
	}
	return rebuildMentionContent(m, botID)
}

func trimLeadingBotMentions(content, botID string) string {
	if strings.TrimSpace(content) == "" || strings.TrimSpace(botID) == "" {
		return strings.TrimSpace(content)
	}

	mention := "<@" + botID + ">"
	mentionNick := "<@!" + botID + ">"
	content = strings.TrimSpace(content)

	for {
		switch {
		case strings.HasPrefix(content, mention):
			content = strings.TrimSpace(strings.TrimPrefix(content, mention))
		case strings.HasPrefix(content, mentionNick):
			content = strings.TrimSpace(strings.TrimPrefix(content, mentionNick))
		default:
			return strings.TrimSpace(content)
		}
	}
}

func rebuildMentionContent(m *discordgo.MessageCreate, botID string) string {
	if m == nil {
		return ""
	}

	tokens := make([]string, 0, len(m.Mentions)+len(m.MentionRoles)+1)
	seen := make(map[string]struct{})

	appendToken := func(token string) {
		token = strings.TrimSpace(token)
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}

	for _, user := range m.Mentions {
		if user == nil || strings.TrimSpace(user.ID) == "" || user.ID == botID {
			continue
		}
		appendToken("<@" + user.ID + ">")
	}
	for _, roleID := range m.MentionRoles {
		if strings.TrimSpace(roleID) == "" {
			continue
		}
		appendToken("<@&" + roleID + ">")
	}
	if m.MentionEveryone {
		appendToken("@everyone")
	}

	return strings.Join(tokens, " ")
}
