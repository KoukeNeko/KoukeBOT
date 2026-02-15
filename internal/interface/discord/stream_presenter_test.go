package discord

import (
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestDiscordStreamPresenterToolDoneResetsBuffer(t *testing.T) {
	presenter := newDiscordStreamPresenter(&Handler{}, "ch", "msg")
	presenter.fullResponse.WriteString("partial")

	done, stop := presenter.processChunk(llm.StreamChunk{ToolStatus: "tool_done"})
	if done {
		t.Fatalf("done = true, want false")
	}
	if stop {
		t.Fatalf("stop = true, want false")
	}
	if presenter.fullResponse.Len() != 0 {
		t.Fatalf("buffer len = %d, want 0", presenter.fullResponse.Len())
	}
}

func TestDiscordStreamPresenterThrottleSkipsImmediateFlush(t *testing.T) {
	presenter := newDiscordStreamPresenter(&Handler{}, "ch", "msg")
	presenter.lastUpdate = time.Now()

	done, stop := presenter.processChunk(llm.StreamChunk{Content: "hello"})
	if done {
		t.Fatalf("done = true, want false")
	}
	if stop {
		t.Fatalf("stop = true, want false")
	}
	if presenter.fullResponse.String() != "hello" {
		t.Fatalf("buffer = %q, want hello", presenter.fullResponse.String())
	}
}

func TestSplitMessageRespectsMaxLength(t *testing.T) {
	segments := splitMessage("12345\n67890\nabcde", 6)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	for i, segment := range segments {
		if len(segment) > 6 {
			t.Fatalf("segment %d too long: len=%d", i, len(segment))
		}
	}
}

func TestFormatToolStartStatusMessage(t *testing.T) {
	msg := formatToolStartStatusMessage("web_search", map[string]interface{}{"query": "golang"})
	if !strings.Contains(msg, "golang") {
		t.Fatalf("unexpected tool status message: %q", msg)
	}
}
