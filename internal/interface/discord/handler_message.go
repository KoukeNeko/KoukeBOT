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

// isReply checks if the message is a Discord reply.
func isReply(m *discordgo.MessageCreate) bool {
	if m == nil || m.MessageReference == nil {
		return false
	}
	return strings.TrimSpace(m.MessageReference.MessageID) != ""
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
