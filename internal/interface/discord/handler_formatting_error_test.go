package discord

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatDiscordErrorQueueTimeout(t *testing.T) {
	msg := formatDiscordError(errors.New("all OAuth account(s) exhausted (queue retry failed: request timed out waiting in queue)"))
	if !strings.Contains(msg, "等待逾時") {
		t.Fatalf("expected queue-timeout guidance, got: %s", msg)
	}
	if strings.Contains(msg, "❌ **發生錯誤**") {
		t.Fatalf("expected specific message instead of generic fallback, got: %s", msg)
	}
}
