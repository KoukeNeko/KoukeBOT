package discord

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

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

func TestDiscordStreamPresenterToolGuardSendsDetailMessage(t *testing.T) {
	sender := &fakeMessageSender{}
	presenter := newDiscordStreamPresenterWithSender(&Handler{}, "ch", "reply-1", sender)

	done, stop := presenter.processChunk(llm.StreamChunk{
		ToolStatus: "tool_guard",
		ToolName:   "web_search",
		ToolInput:  map[string]interface{}{"query": "same"},
		ToolError:  `Tool guard: duplicate call blocked for "web_search" with identical input in this turn.`,
	})
	if done {
		t.Fatalf("done = true, want false")
	}
	if stop {
		t.Fatalf("stop = true, want false")
	}
	if len(sender.sendContents) != 1 {
		t.Fatalf("send contents = %d, want 1", len(sender.sendContents))
	}
	if !strings.Contains(sender.sendContents[0], "[TOOL_GUARD_DETAIL]") {
		t.Fatalf("expected guard detail prefix in message, got: %q", sender.sendContents[0])
	}
}

func TestDiscordStreamPresenterSendFinalLogsWarningWhenEditFails(t *testing.T) {
	// When edit fails on sendFinal, we intentionally do NOT fall back to a new
	// send — the existing streaming message already contains partial content and
	// a duplicate send would create a second identical message in the channel.
	sender := &fakeMessageSender{
		editErr: errors.New("edit failed"),
	}
	presenter := newDiscordStreamPresenterWithSender(&Handler{}, "ch", "reply-1", sender)
	presenter.messageID = "existing-msg"
	presenter.fullResponse.WriteString("hello world")

	presenter.sendFinal()

	if sender.editCalls != 1 {
		t.Fatalf("edit calls = %d, want 1", sender.editCalls)
	}
	// No fallback send — leaves the existing message in place.
	if len(sender.sendWithReturnContents) != 0 {
		t.Fatalf("unexpected fallback sendWithReturn calls = %d, want 0", len(sender.sendWithReturnContents))
	}
	if presenter.messageID != "existing-msg" {
		t.Fatalf("messageID should remain unchanged, got %q", presenter.messageID)
	}
}

func TestDiscordStreamPresenterSendOrUpdateSkipsFallbackWhenEditFails(t *testing.T) {
	// During streaming, edit failure is silently swallowed and NOT followed by a
	// new send — doing so would create a duplicate message once the stream ends.
	sender := &fakeMessageSender{
		editErr: errors.New("edit failed"),
	}
	presenter := newDiscordStreamPresenterWithSender(&Handler{}, "ch", "reply-1", sender)
	presenter.messageID = "existing-msg"

	if err := presenter.sendOrUpdate("updated content"); err != nil {
		t.Fatalf("sendOrUpdate error = %v, want nil", err)
	}
	if sender.editCalls != 1 {
		t.Fatalf("edit calls = %d, want 1", sender.editCalls)
	}
	// No new send — incremental update is simply skipped.
	if len(sender.sendWithReturnContents) != 0 {
		t.Fatalf("unexpected fallback sendWithReturn calls = %d, want 0", len(sender.sendWithReturnContents))
	}
}

func TestDiscordStreamPresenterProcessChunkContinuesWhenEditFails(t *testing.T) {
	// During streaming, an edit failure does NOT stop the stream — sendOrUpdate
	// swallows the error and returns nil so the stream can continue uninterrupted.
	sender := &fakeMessageSender{
		editErr: errors.New("edit failed"),
	}
	presenter := newDiscordStreamPresenterWithSender(&Handler{}, "ch", "reply-1", sender)
	presenter.messageID = "existing-msg"
	presenter.lastUpdate = time.Now().Add(-2 * time.Second)

	done, stop := presenter.processChunk(llm.StreamChunk{Content: "partial"})
	if done {
		t.Fatalf("done = true, want false")
	}
	if stop {
		t.Fatalf("stop = true, want false")
	}
	if sender.editCalls != 1 {
		t.Fatalf("edit calls = %d, want 1", sender.editCalls)
	}
	if len(sender.sendWithReturnContents) != 0 {
		t.Fatalf("expected no fallback sends, got %d", len(sender.sendWithReturnContents))
	}
}

type fakeMessageSender struct {
	editErr           error
	sendErr           error
	sendWithReturnErr error
	replyErr          error

	editCalls              int
	sendContents           []string
	sendWithReturnContents []string
	replyContents          []string
	nextID                 int
}

func (f *fakeMessageSender) SendMessage(_ string, content string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sendContents = append(f.sendContents, content)
	return nil
}

func (f *fakeMessageSender) SendMessageWithReturn(_ string, content string) (*discordgo.Message, error) {
	if f.sendWithReturnErr != nil {
		return nil, f.sendWithReturnErr
	}
	f.sendWithReturnContents = append(f.sendWithReturnContents, content)
	f.nextID++
	return &discordgo.Message{ID: fmt.Sprintf("msg-%d", f.nextID)}, nil
}

func (f *fakeMessageSender) SendMessageReply(_ string, content, _ string) (*discordgo.Message, error) {
	if f.replyErr != nil {
		return nil, f.replyErr
	}
	f.replyContents = append(f.replyContents, content)
	f.nextID++
	return &discordgo.Message{ID: fmt.Sprintf("reply-%d", f.nextID)}, nil
}

func (f *fakeMessageSender) EditMessage(_, _, _ string) error {
	f.editCalls++
	return f.editErr
}
