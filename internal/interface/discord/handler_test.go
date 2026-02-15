package discord

import (
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
	"github.com/bwmarrin/discordgo"
	toon "github.com/toon-format/toon-go"
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

	eventType, ok := tooncodec.DecodeEventType(entry.FormattedText)
	if !ok || eventType != tooncodec.EventTypeMessage {
		t.Fatalf("expected TOON msg event, got: %s", entry.FormattedText)
	}
	decoded, err := toon.DecodeString(entry.FormattedText)
	if err != nil {
		t.Fatalf("decode TOON reply entry: %v", err)
	}
	msg, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("reply entry decoded type = %T, want map[string]any", decoded)
	}
	if got := msg["pri"]; got != tooncodec.PriorityReplyHigh {
		t.Fatalf("reply priority = %v, want %s", got, tooncodec.PriorityReplyHigh)
	}
	if got := msg["txt"]; got != "new message" {
		t.Fatalf("reply content = %v, want new message", got)
	}
}

func TestBuildContextForLLMEncodesTOONContext(t *testing.T) {
	history, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    "discord",
		UserID:    "u1",
		Name:      "alice",
		ChannelID: "ch-1",
		PersonaID: "persona-1",
		Timestamp: time.Unix(100, 0).UTC(),
		Text:      "history",
	})
	if err != nil {
		t.Fatalf("encode history event: %v", err)
	}
	current, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    "discord",
		UserID:    "u2",
		Name:      "bob",
		ChannelID: "ch-1",
		PersonaID: "persona-1",
		Timestamp: time.Unix(200, 0).UTC(),
		Text:      "current",
	})
	if err != nil {
		t.Fatalf("encode current event: %v", err)
	}

	ctxPayload := buildContextForLLM([]domain.ChannelHistoryEntry{{FormattedText: history}}, current, "ch-1", "persona-1")
	eventType, ok := tooncodec.DecodeEventType(ctxPayload)
	if !ok || eventType != tooncodec.EventTypeContext {
		t.Fatalf("event type = %q (ok=%t), want %q", eventType, ok, tooncodec.EventTypeContext)
	}
	decoded, err := toon.DecodeString(ctxPayload)
	if err != nil {
		t.Fatalf("decode ctx payload: %v", err)
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded ctx type = %T, want map[string]any", decoded)
	}
	cur, ok := obj["cur"].(map[string]any)
	if !ok || cur["txt"] != "current" {
		t.Fatalf("cur.txt = %v, want current", cur["txt"])
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
