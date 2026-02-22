package logger

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateDiscordContentNoopWhenWithinLimit(t *testing.T) {
	in := "hello"
	got := truncateDiscordContent(in, 10)
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestTruncateDiscordContentTruncatesWithSuffix(t *testing.T) {
	in := strings.Repeat("a", 80)
	got := truncateDiscordContent(in, 40)
	if utf8.RuneCountInString(got) != 40 {
		t.Fatalf("len(got) = %d, want 40", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, discordTruncateSuffix) {
		t.Fatalf("expected suffix %q in %q", discordTruncateSuffix, got)
	}
}

func TestTruncateDiscordContentHandlesUnicodeRunes(t *testing.T) {
	in := strings.Repeat("你", 20)
	got := truncateDiscordContent(in, 10)
	if utf8.RuneCountInString(got) != 10 {
		t.Fatalf("rune count = %d, want 10", utf8.RuneCountInString(got))
	}
}
