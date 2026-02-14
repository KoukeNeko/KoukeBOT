package discord

import (
	"strings"
	"testing"
	"time"

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

	if entry := h.buildReplyReferenceEntry(nil, oldReply, &cutoff); entry != nil {
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

	entry := h.buildReplyReferenceEntry(nil, newReply, &cutoff)
	if entry == nil {
		t.Fatal("expected newer reply to be included")
	}
	if !strings.Contains(entry.FormattedText, "Replied message - high priority") {
		t.Fatalf("expected high-priority marker in reply entry, got: %s", entry.FormattedText)
	}
	if !strings.Contains(entry.FormattedText, "new message") {
		t.Fatalf("expected reply content to be included, got: %s", entry.FormattedText)
	}
}
