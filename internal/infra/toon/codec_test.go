package tooncodec

import (
	"strings"
	"testing"
	"time"

	toon "github.com/toon-format/toon-go"
)

func TestEncodeMessageEventProducesTOON(t *testing.T) {
	ts := time.Unix(1739612040, 0).UTC()
	encoded, err := EncodeMessageEvent(MessageEventInput{
		Source:    "discord",
		UserID:    "123",
		Name:      "alice",
		ChannelID: "ch1",
		PersonaID: "p1",
		Timestamp: ts,
		Text:      "hello, world",
		Priority:  PriorityReplyHigh,
	})
	if err != nil {
		t.Fatalf("EncodeMessageEvent: %v", err)
	}
	eventType, ok := DecodeEventType(encoded)
	if !ok || eventType != EventTypeMessage {
		t.Fatalf("event type = %q (ok=%t), want %q", eventType, ok, EventTypeMessage)
	}
	decoded, err := toon.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode TOON: %v", err)
	}
	obj := decoded.(map[string]any)
	if got := obj["uid"]; got != "123" {
		t.Fatalf("uid = %v, want 123", got)
	}
	if got := obj["txt"]; got != "hello, world" {
		t.Fatalf("txt = %v, want hello, world", got)
	}
	if got := obj["pri"]; got != PriorityReplyHigh {
		t.Fatalf("pri = %v, want %s", got, PriorityReplyHigh)
	}
}

func TestEncodeContextEventProducesTOONCtx(t *testing.T) {
	msg1, err := EncodeMessageEvent(MessageEventInput{Source: "discord", UserID: "u1", Text: "older", ChannelID: "c", PersonaID: "p", Timestamp: time.Unix(1, 0).UTC()})
	if err != nil {
		t.Fatalf("EncodeMessageEvent msg1: %v", err)
	}
	msg2, err := EncodeMessageEvent(MessageEventInput{Source: "discord", UserID: "u2", Text: "latest", ChannelID: "c", PersonaID: "p", Timestamp: time.Unix(2, 0).UTC()})
	if err != nil {
		t.Fatalf("EncodeMessageEvent msg2: %v", err)
	}

	ctx, err := EncodeContextEvent(ContextEventInput{
		Source:    "discord",
		ChannelID: "c",
		PersonaID: "p",
		History:   []string{msg1},
		Current:   msg2,
	})
	if err != nil {
		t.Fatalf("EncodeContextEvent: %v", err)
	}

	eventType, ok := DecodeEventType(ctx)
	if !ok || eventType != EventTypeContext {
		t.Fatalf("event type = %q (ok=%t), want %q", eventType, ok, EventTypeContext)
	}
	decoded, err := toon.DecodeString(ctx)
	if err != nil {
		t.Fatalf("decode TOON ctx: %v", err)
	}
	obj := decoded.(map[string]any)
	hist, ok := obj["hist"].([]any)
	if !ok || len(hist) != 1 {
		t.Fatalf("hist len = %d, want 1", len(hist))
	}
	cur, ok := obj["cur"].(map[string]any)
	if !ok {
		t.Fatalf("cur type = %T, want map[string]any", obj["cur"])
	}
	if got := cur["txt"]; got != "latest" {
		t.Fatalf("cur.txt = %v, want latest", got)
	}
}

func TestEncodeToolResultsEventDeterministic(t *testing.T) {
	items := []ToolResultInput{{ID: "1", Name: "discord_observe", OK: true, Out: "ok"}}
	a, err := EncodeToolResultsEvent(items)
	if err != nil {
		t.Fatalf("EncodeToolResultsEvent first: %v", err)
	}
	b, err := EncodeToolResultsEvent(items)
	if err != nil {
		t.Fatalf("EncodeToolResultsEvent second: %v", err)
	}
	if a != b {
		t.Fatalf("encoding should be deterministic\nA=%s\nB=%s", a, b)
	}
}

func TestHumanizeTOONFallback(t *testing.T) {
	raw := "not toon"
	if got := HumanizeTOON(raw); got != raw {
		t.Fatalf("humanize fallback = %q, want original", got)
	}
}

func TestExtractMessageText(t *testing.T) {
	encoded, err := EncodeMessageEvent(MessageEventInput{
		Source:    "api",
		UserID:    "u1",
		ChannelID: "ch",
		PersonaID: "persona-a",
		Timestamp: time.Unix(10, 0).UTC(),
		Text:      "hello extract",
		Priority:  PriorityNormal,
	})
	if err != nil {
		t.Fatalf("EncodeMessageEvent: %v", err)
	}

	text, ok := ExtractMessageText(encoded)
	if !ok {
		t.Fatalf("ExtractMessageText should succeed")
	}
	if text != "hello extract" {
		t.Fatalf("ExtractMessageText = %q, want %q", text, "hello extract")
	}

	if _, ok := ExtractMessageText("not-toon"); ok {
		t.Fatalf("ExtractMessageText should fail for plain text")
	}
}

func TestEncodeMessageEventEscapesSpecialCharacters(t *testing.T) {
	txt := "line1\nline2,with,comma"
	encoded, err := EncodeMessageEvent(MessageEventInput{Source: "api", Text: txt, Timestamp: time.Unix(10, 0).UTC()})
	if err != nil {
		t.Fatalf("EncodeMessageEvent: %v", err)
	}
	if !strings.Contains(encoded, "txt") {
		t.Fatalf("encoded message should contain txt field: %s", encoded)
	}
	decoded, err := toon.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode encoded text: %v", err)
	}
	obj := decoded.(map[string]any)
	if got := obj["txt"]; got != txt {
		t.Fatalf("decoded txt = %v, want %q", got, txt)
	}
}
