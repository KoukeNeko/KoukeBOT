package discord

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestParseStrictReminder(t *testing.T) {
	delay, text, ok := parseStrictReminder("10分鐘後叫我喝水")
	if !ok {
		t.Fatalf("expected strict reminder match")
	}
	if delay != 10*time.Minute {
		t.Fatalf("delay=%s, want 10m", delay)
	}
	if text != "喝水" {
		t.Fatalf("text=%q, want 喝水", text)
	}
}

func TestParseStrictMonitor(t *testing.T) {
	duration, ok := parseStrictMonitor("別讓我看到你在接下來的 8 小時內有任何活動紀錄")
	if !ok {
		t.Fatalf("expected strict monitor match")
	}
	if duration != 8*time.Hour {
		t.Fatalf("duration=%s, want 8h", duration)
	}
}

func TestParseStrictMonitor_NoMatchFallsBack(t *testing.T) {
	if _, ok := parseStrictMonitor("請幫我整理今天的對話"); ok {
		t.Fatalf("expected strict monitor to not match unrelated message")
	}
}

func TestStrictMonitorTargetsIncludeRequesterAndAllMentions(t *testing.T) {
	msg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Author: &discordgo.User{ID: "u-request"},
			Mentions: []*discordgo.User{
				{ID: "u-target-1"},
				{ID: "u-target-2"},
				{ID: "u-target-1"},
				{ID: "u-bot"},
			},
		},
	}
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "u-bot"},
			},
		},
	}

	targets := strictMonitorTargets(msg, session)
	if len(targets) != 3 {
		t.Fatalf("targets len=%d, want 3", len(targets))
	}
	if targets[0] != "u-request" {
		t.Fatalf("targets[0]=%q, want u-request", targets[0])
	}
}
