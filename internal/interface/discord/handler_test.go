package discord

import (
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/bwmarrin/discordgo"
)

func TestShouldIncludeDiscordTimestamp(t *testing.T) {
	cutoff := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)

	if !shouldIncludeDiscordTimestamp(cutoff.Add(-time.Hour), nil) {
		t.Fatal("expected message to be included when cutoff is nil")
	}
	if shouldIncludeDiscordTimestamp(cutoff.Add(-time.Second), &cutoff) {
		t.Fatal("expected message before cutoff to be excluded")
	}
	if shouldIncludeDiscordTimestamp(cutoff, &cutoff) {
		t.Fatal("expected message at cutoff boundary to be excluded")
	}
	if !shouldIncludeDiscordTimestamp(cutoff.Add(time.Second), &cutoff) {
		t.Fatal("expected message after cutoff to be included")
	}
}

func TestHistoryRecentOffset(t *testing.T) {
	total := 24
	if got := historyRecentOffset(total, total-1); got != 0 {
		t.Fatalf("newest message offset = %d, want 0", got)
	}
	if got := historyRecentOffset(total, total-2); got != 1 {
		t.Fatalf("second newest message offset = %d, want 1", got)
	}
	if got := historyRecentOffset(total, 0); got != total-1 {
		t.Fatalf("oldest message offset = %d, want %d", got, total-1)
	}
}

func TestBuildReplyReferenceEntryRespectsCutoff(t *testing.T) {
	h := &Handler{}
	cutoff := time.Date(2026, time.February, 14, 10, 0, 0, 0, time.UTC)

	oldReply := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "ch-1",
			MessageReference: &discordgo.MessageReference{
				MessageID: "msg-old",
			},
			ReferencedMessage: &discordgo.Message{
				ID:        "msg-old",
				Content:   "older message",
				Timestamp: cutoff.Add(-time.Second),
				Author: &discordgo.User{
					ID:       "u1",
					Username: "alice",
				},
			},
		},
	}

	if entry := h.buildReplyReferenceEntry(nil, oldReply, "persona-1", &cutoff); entry != nil {
		t.Fatalf("expected old reply to be excluded by cutoff, got %+v", entry)
	}

	newReply := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "ch-1",
			MessageReference: &discordgo.MessageReference{
				MessageID: "msg-new",
			},
			ReferencedMessage: &discordgo.Message{
				ID:        "msg-new",
				Content:   "new message",
				Timestamp: cutoff.Add(2 * time.Second),
				Author: &discordgo.User{
					ID:       "u2",
					Username: "bob",
				},
			},
		},
	}

	entry := h.buildReplyReferenceEntry(nil, newReply, "persona-1", &cutoff)
	if entry == nil {
		t.Fatal("expected newer reply to be included")
	}
	if !strings.HasPrefix(entry.FormattedText, "[↩ bob · ") {
		t.Fatalf("reply entry should start with '[↩ bob · HH:mm]', got: %s", entry.FormattedText)
	}
	if !strings.Contains(entry.FormattedText, "new message") {
		t.Fatalf("reply entry should contain message content, got: %s", entry.FormattedText)
	}
	if entry.IsBot {
		t.Fatalf("reply author is not the bot, IsBot should be false")
	}
}

func TestBuildEphemeralHistoryCreatesNativeMessages(t *testing.T) {
	entries := []domain.ChannelHistoryEntry{
		{FormattedText: "[alice · 10:00] hello", IsBot: false},
		{FormattedText: "I am the bot response", IsBot: true},
		{FormattedText: "   ", IsBot: false}, // empty — should be skipped
	}

	messages := buildEphemeralHistory(entries)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (empty entry skipped), got %d", len(messages))
	}
	if messages[0].Role != domain.RoleUser {
		t.Fatalf("first message role = %s, want user", messages[0].Role)
	}
	if messages[0].Content != "[alice · 10:00] hello" {
		t.Fatalf("first message content = %q", messages[0].Content)
	}
	if messages[1].Role != domain.RoleAssistant {
		t.Fatalf("second message role = %s, want assistant", messages[1].Role)
	}
	if messages[1].Content != "I am the bot response" {
		t.Fatalf("second message content = %q", messages[1].Content)
	}
}

func TestCleanMentionStripsBotPrefixButPreservesOtherUserTags(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@bot-1> 請找 <@user-2> 與 <@!user-3>",
			Mentions: []*discordgo.User{
				{ID: "bot-1"},
				{ID: "user-2"},
				{ID: "user-3"},
			},
		},
	}

	got := cleanMention(session, message, true)
	if strings.Contains(got, "<@bot-1>") {
		t.Fatalf("bot mention should be removed from leading prefix: %q", got)
	}
	if !strings.Contains(got, "<@user-2>") {
		t.Fatalf("expected user-2 mention to remain: %q", got)
	}
	if !strings.Contains(got, "<@!user-3>") {
		t.Fatalf("expected user-3 nickname mention to remain: %q", got)
	}
}

func TestCleanMentionPreservesBotMentionWhenStripDisabled(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "<@bot-1> 請找 <@user-2>",
			Mentions: []*discordgo.User{
				{ID: "bot-1"},
				{ID: "user-2"},
			},
		},
	}

	got := cleanMention(session, message, false)
	if !strings.Contains(got, "<@bot-1>") {
		t.Fatalf("bot mention should be preserved when strip is disabled: %q", got)
	}
	if !strings.Contains(got, "<@user-2>") {
		t.Fatalf("user mention should still be present: %q", got)
	}
}

func TestCleanMentionRebuildsMentionsWhenContentEmpty(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "",
			Mentions: []*discordgo.User{
				{ID: "bot-1"},
				{ID: "user-2"},
			},
			MentionRoles:    []string{"role-9"},
			MentionEveryone: true,
		},
	}

	got := cleanMention(session, message, true)
	if !strings.Contains(got, "<@user-2>") {
		t.Fatalf("expected user mention rebuilt, got: %q", got)
	}
	if !strings.Contains(got, "<@&role-9>") {
		t.Fatalf("expected role mention rebuilt, got: %q", got)
	}
	if !strings.Contains(got, "@everyone") {
		t.Fatalf("expected @everyone rebuilt, got: %q", got)
	}
	if strings.Contains(got, "<@bot-1>") {
		t.Fatalf("bot mention should not be rebuilt, got: %q", got)
	}
}

func TestFilterIncomingMessageInjectsAttachmentNarrationForMentionOnlyImage(t *testing.T) {
	h := &Handler{stripLeadingBotMentionPrefix: false}
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "<@bot-1>",
			Author: &discordgo.User{
				ID:       "user-1",
				Username: "alice",
			},
			Mentions: []*discordgo.User{
				{ID: "bot-1"},
			},
			Attachments: []*discordgo.MessageAttachment{
				{Filename: "photo.png", ContentType: "image/png"},
			},
		},
	}

	content, ok := h.filterIncomingMessage(session, message)
	if !ok {
		t.Fatal("expected mention-only image message to be accepted")
	}
	if !strings.Contains(content, "alice 傳送了 1 張圖片") {
		t.Fatalf("expected sender narration for image-only input, got: %q", content)
	}
	if !strings.Contains(content, "<media:image> (1 image)") {
		t.Fatalf("expected media placeholder in narration, got: %q", content)
	}
}

func TestFilterIncomingMessageKeepsTextWhenMentionWithCaption(t *testing.T) {
	h := &Handler{stripLeadingBotMentionPrefix: false}
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "<@bot-1> 這是圖片說明",
			Author: &discordgo.User{
				ID:       "user-1",
				Username: "alice",
			},
			Mentions: []*discordgo.User{
				{ID: "bot-1"},
			},
			Attachments: []*discordgo.MessageAttachment{
				{Filename: "photo.png", ContentType: "image/png"},
			},
		},
	}

	content, ok := h.filterIncomingMessage(session, message)
	if !ok {
		t.Fatal("expected mention+caption image message to be accepted")
	}
	if content != "<@bot-1> 這是圖片說明" {
		t.Fatalf("expected original text to be preserved, got: %q", content)
	}
}

func TestFilterIncomingMessageAcceptsReplyWithoutMention(t *testing.T) {
	h := &Handler{stripLeadingBotMentionPrefix: true}
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID:  "guild-1",
			Content:  "這是回覆內容",
			Author:   &discordgo.User{ID: "user-1", Username: "alice"},
			Mentions: []*discordgo.User{},
			MessageReference: &discordgo.MessageReference{
				MessageID: "ref-1",
			},
		},
	}

	content, ok := h.filterIncomingMessage(session, message)
	if !ok {
		t.Fatal("expected reply message to be accepted without mention")
	}
	if content != "這是回覆內容" {
		t.Fatalf("unexpected reply content: %q", content)
	}
}
