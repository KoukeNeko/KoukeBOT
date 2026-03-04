package api

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatUserFriendlyErrorQueueTimeout(t *testing.T) {
	msg := formatUserFriendlyError(errors.New("all OAuth account(s) exhausted (queue retry failed: request timed out waiting in queue)"))
	if !strings.Contains(msg, "自動重試佇列") {
		t.Fatalf("expected queue-timeout guidance, got: %s", msg)
	}
	if strings.Contains(msg, "❌ 發生錯誤") {
		t.Fatalf("expected specific message instead of generic fallback, got: %s", msg)
	}
}

func TestFormatUserFriendlyErrorQueueFull(t *testing.T) {
	msg := formatUserFriendlyError(errors.New("request queue full — please try again later"))
	if !strings.Contains(msg, "等候佇列已滿") {
		t.Fatalf("expected queue-full guidance, got: %s", msg)
	}
	if strings.Contains(msg, "❌ 發生錯誤") {
		t.Fatalf("expected specific message instead of generic fallback, got: %s", msg)
	}
}
