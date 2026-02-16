package chat

import (
	"context"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
	toon "github.com/toon-format/toon-go"
)

func TestEncodeAssistantMessageForStorageUsesBotPersonaUID(t *testing.T) {
	service := &ChatService{}
	ctx := llm.WithRequestContext(context.Background(), llm.RequestContext{
		Source:    llm.RequestSourceDiscord,
		ChannelID: "ch-discord",
		PersonaID: "persona-a",
	})
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta: domain.PersonaMeta{
				ID:   "persona-a",
				Name: "Satsuki",
			},
		},
	}

	encoded := service.encodeAssistantMessageForStorage(ctx, "fallback-channel", persona, "persona-a", "assistant reply")
	eventType, ok := tooncodec.DecodeEventType(encoded)
	if !ok || eventType != tooncodec.EventTypeMessage {
		t.Fatalf("expected TOON msg event, got: %s", encoded)
	}

	decoded, err := toon.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}
	obj, ok := decoded.(map[string]interface{})
	if !ok {
		t.Fatalf("decoded object type = %T, want map", decoded)
	}

	if got, _ := obj["uid"].(string); got != "bot:persona-a" {
		t.Fatalf("uid = %q, want %q", got, "bot:persona-a")
	}
	if got, _ := obj["pid"].(string); got != "persona-a" {
		t.Fatalf("pid = %q, want %q", got, "persona-a")
	}
	if got, _ := obj["name"].(string); got != "Satsuki" {
		t.Fatalf("name = %q, want %q", got, "Satsuki")
	}
	if got, _ := obj["cid"].(string); got != "ch-discord" {
		t.Fatalf("cid = %q, want %q", got, "ch-discord")
	}
	if got, _ := obj["src"].(string); got != llm.RequestSourceDiscord {
		t.Fatalf("src = %q, want %q", got, llm.RequestSourceDiscord)
	}
	if got, _ := obj["txt"].(string); got != "assistant reply" {
		t.Fatalf("txt = %q, want %q", got, "assistant reply")
	}
}

func TestEncodeUserMessageForStorageReturnsTOONMessage(t *testing.T) {
	service := &ChatService{}
	ctx := llm.WithRequestContext(context.Background(), llm.RequestContext{
		Source:    llm.RequestSourceWeb,
		ChannelID: "web-test",
		UserID:    "user-1",
		PersonaID: "persona-a",
	})

	encoded := service.encodeUserMessageForStorage(ctx, "web-test", "persona-a", "hello user", "")
	eventType, ok := tooncodec.DecodeEventType(encoded)
	if !ok || eventType != tooncodec.EventTypeMessage {
		t.Fatalf("expected TOON msg event, got: %s", encoded)
	}

	decoded, err := toon.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}
	obj := decoded.(map[string]interface{})
	if got, _ := obj["uid"].(string); got != "user-1" {
		t.Fatalf("uid = %q, want %q", got, "user-1")
	}
	if got, _ := obj["txt"].(string); got != "hello user" {
		t.Fatalf("txt = %q, want %q", got, "hello user")
	}
}

func TestEncodeUserMessageForStorageCanRecoverFromContextEventFallback(t *testing.T) {
	service := &ChatService{}
	ctx := context.Background()

	current, err := tooncodec.EncodeMessageEvent(tooncodec.MessageEventInput{
		Source:    "discord",
		UserID:    "u1",
		ChannelID: "ch",
		PersonaID: "persona-a",
		Timestamp: time.Date(2026, time.February, 15, 9, 0, 0, 0, time.UTC),
		Text:      "current message",
		Priority:  tooncodec.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("EncodeMessageEvent failed: %v", err)
	}
	ctxEvent, err := tooncodec.EncodeContextEvent(tooncodec.ContextEventInput{
		Source:    "discord",
		ChannelID: "ch",
		PersonaID: "persona-a",
		Current:   current,
	})
	if err != nil {
		t.Fatalf("EncodeContextEvent failed: %v", err)
	}

	encoded := service.encodeUserMessageForStorage(ctx, "ch", "persona-a", "", ctxEvent)
	eventType, ok := tooncodec.DecodeEventType(encoded)
	if !ok || eventType != tooncodec.EventTypeMessage {
		t.Fatalf("expected recovered TOON msg event, got: %s", encoded)
	}
}
