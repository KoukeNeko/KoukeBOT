package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func TestStreamWithMultimodalHistoryRetriesReducedContextOnTimeout(t *testing.T) {
	provider := &multimodalRetryTestProvider{
		multimodalErrors: []error{
			fmt.Errorf("code assist multimodal stream: %w", context.DeadlineExceeded),
			nil,
		},
		multimodalStreams: [][]llm.StreamChunk{
			nil,
			{
				{Content: "[Status]: Stable\n[Analysis]: Retry succeeded.\n[Response]: retry ok"},
				{Done: true, TokensUsed: 21},
			},
		},
	}
	service := newMultimodalRetryTestService(provider)
	conv := &domain.Conversation{
		ID:        "conv-mm-retry",
		ChannelID: "channel-mm",
		PersonaID: "persona-mm",
	}
	persona := multimodalRetryPersona("persona-mm")

	ctx := llm.WithRequestContext(context.Background(), llm.RequestContext{
		Source:    llm.RequestSourceDiscord,
		ChannelID: conv.ChannelID,
		UserID:    "user-mm",
		PersonaID: conv.PersonaID,
	})

	historyEntries := []domain.ChannelHistoryEntry{
		{
			FormattedText: "history-entry-1",
			Images: []domain.ImageInput{
				{Filename: "hist.jpg", MimeType: "image/jpeg", Data: []byte{1, 2, 3}},
			},
		},
	}
	currentImages := []domain.ImageInput{
		{Filename: "current.jpg", MimeType: "image/jpeg", Data: []byte{9, 8, 7}},
	}

	streamCh, err := service.streamWithMultimodalHistory(
		ctx,
		conv,
		persona,
		conv.PersonaID,
		"ctx-payload",
		"current user input",
		currentImages,
		historyEntries,
	)
	if err != nil {
		t.Fatalf("streamWithMultimodalHistory() error = %v", err)
	}

	var output strings.Builder
	for chunk := range streamCh {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
		output.WriteString(chunk.Content)
	}
	if !strings.Contains(output.String(), "retry ok") {
		t.Fatalf("final output should contain retry result, got: %s", output.String())
	}

	if len(provider.calls) != 2 {
		t.Fatalf("multimodal call count = %d, want 2", len(provider.calls))
	}

	firstCurrent := lastMultimodalMessage(provider.calls[0].messages)
	if !partsContainExactText(firstCurrent.Parts, historyEntries[0].FormattedText) {
		t.Fatalf("first attempt should include history text")
	}
	if partsContainExactText(firstCurrent.Parts, "ctx-payload") {
		t.Fatalf("first attempt should not duplicate context payload when history entries are present")
	}
	if !partsContainTOONMessageText(firstCurrent.Parts, "current user input") {
		t.Fatalf("first attempt should include current TOON message")
	}
	if countImagesInParts(firstCurrent.Parts) != 2 {
		t.Fatalf("first attempt image count = %d, want 2 (history + current)", countImagesInParts(firstCurrent.Parts))
	}

	secondCurrent := lastMultimodalMessage(provider.calls[1].messages)
	if partsContainExactText(secondCurrent.Parts, historyEntries[0].FormattedText) {
		t.Fatalf("reduced retry should drop history text")
	}
	if !partsContainTOONMessageText(secondCurrent.Parts, "current user input") {
		t.Fatalf("reduced retry should keep current TOON message")
	}
	if countImagesInParts(secondCurrent.Parts) != 1 {
		t.Fatalf("reduced retry image count = %d, want 1 (current only)", countImagesInParts(secondCurrent.Parts))
	}
}

func TestStreamWithMultimodalHistoryDoesNotRetryNonTimeoutError(t *testing.T) {
	provider := &multimodalRetryTestProvider{
		multimodalErrors: []error{errors.New("code assist multimodal stream HTTP 500")},
	}
	service := newMultimodalRetryTestService(provider)
	conv := &domain.Conversation{ID: "conv-mm-no-retry", ChannelID: "ch", PersonaID: "persona-mm"}
	persona := multimodalRetryPersona("persona-mm")

	_, err := service.streamWithMultimodalHistory(
		context.Background(),
		conv,
		persona,
		conv.PersonaID,
		"ctx",
		"hello",
		nil,
		nil,
	)
	if err == nil {
		t.Fatalf("expected error for non-timeout multimodal failure")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("multimodal call count = %d, want 1", len(provider.calls))
	}
}

func TestStreamWithMultimodalHistoryDoesNotRetryCanceledParentContext(t *testing.T) {
	provider := &multimodalRetryTestProvider{
		multimodalErrors: []error{context.Canceled},
	}
	service := newMultimodalRetryTestService(provider)
	conv := &domain.Conversation{ID: "conv-mm-canceled", ChannelID: "ch", PersonaID: "persona-mm"}
	persona := multimodalRetryPersona("persona-mm")

	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.streamWithMultimodalHistory(
		parentCtx,
		conv,
		persona,
		conv.PersonaID,
		"ctx",
		"hello",
		nil,
		nil,
	)
	if err == nil {
		t.Fatalf("expected error for canceled context")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("multimodal call count = %d, want 1", len(provider.calls))
	}
}

func TestDeriveInitialMultimodalAttemptContextReservesRetryWindow(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelParent()

	attemptCtx, attemptCancel, retryBudget := deriveInitialMultimodalAttemptContext(parent)
	if attemptCancel == nil {
		t.Fatalf("expected derived attempt cancel function")
	}
	defer attemptCancel()

	if retryBudget != multimodalRetryReserveWindow {
		t.Fatalf("retry budget = %s, want %s", retryBudget, multimodalRetryReserveWindow)
	}
	deadline, ok := attemptCtx.Deadline()
	if !ok {
		t.Fatalf("expected derived attempt deadline")
	}
	remaining := time.Until(deadline)
	expected := 2*time.Minute - multimodalRetryReserveWindow
	tolerance := 3 * time.Second
	if remaining < expected-tolerance || remaining > expected+tolerance {
		t.Fatalf("attempt remaining = %s, want around %s (+/-%s)", remaining, expected, tolerance)
	}
}

func newMultimodalRetryTestService(provider llm.Provider) *ChatService {
	return NewChatService(
		provider,
		NewContextAssembler(),
		store.NewMemoryStore(),
		nil,
		0,
		map[string]*domain.PersonaModule{
			"persona-mm": multimodalRetryPersona("persona-mm"),
		},
		nil,
		nil,
	)
}

func multimodalRetryPersona(id string) *domain.PersonaModule {
	return &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           domain.PersonaMeta{ID: id, Name: "Retry Persona"},
			SystemTemplate: "You are a test persona.\n{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
			Generation: domain.GenerationConfig{
				Temperature: 0.7,
				MaxTokens:   1024,
			},
		},
		Lore: "test lore",
	}
}

type multimodalRetryTestProvider struct {
	calls             []multimodalRetryCall
	multimodalErrors  []error
	multimodalStreams [][]llm.StreamChunk
}

type multimodalRetryCall struct {
	messages []domain.MultimodalMessage
}

func (p *multimodalRetryTestProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *multimodalRetryTestProvider) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("unexpected StreamChat call")
}

func (p *multimodalRetryTestProvider) StreamChatMultimodal(ctx context.Context, _ string, messages []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	p.calls = append(p.calls, multimodalRetryCall{
		messages: cloneMultimodalMessages(messages),
	})
	callIdx := len(p.calls) - 1

	if callIdx < len(p.multimodalErrors) && p.multimodalErrors[callIdx] != nil {
		return nil, p.multimodalErrors[callIdx]
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}

	chunks := []llm.StreamChunk{
		{Content: "[Status]: Stable\n[Analysis]: Default stream.\n[Response]: ok"},
		{Done: true},
	}
	if callIdx < len(p.multimodalStreams) && p.multimodalStreams[callIdx] != nil {
		chunks = p.multimodalStreams[callIdx]
	}

	ch := make(chan llm.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *multimodalRetryTestProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking:  true,
		ContextWindow:     1_000_000,
		SupportsStreaming: true,
		ProviderName:      "multimodal-retry-test",
	}
}

func (p *multimodalRetryTestProvider) RegisterTools(_ []domain.Tool) {}

func cloneMultimodalMessages(messages []domain.MultimodalMessage) []domain.MultimodalMessage {
	out := make([]domain.MultimodalMessage, 0, len(messages))
	for _, msg := range messages {
		cloneParts := make([]domain.MessagePart, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			clonePart := domain.MessagePart{Text: part.Text}
			if part.ImageData != nil {
				img := domain.ImageInput{
					Data:     append([]byte(nil), part.ImageData.Data...),
					MimeType: part.ImageData.MimeType,
					Filename: part.ImageData.Filename,
				}
				clonePart.ImageData = &img
			}
			cloneParts = append(cloneParts, clonePart)
		}
		out = append(out, domain.MultimodalMessage{
			Role:      msg.Role,
			Parts:     cloneParts,
			Timestamp: msg.Timestamp,
		})
	}
	return out
}

func lastMultimodalMessage(messages []domain.MultimodalMessage) domain.MultimodalMessage {
	if len(messages) == 0 {
		return domain.MultimodalMessage{}
	}
	return messages[len(messages)-1]
}

func partsContainExactText(parts []domain.MessagePart, expected string) bool {
	for _, part := range parts {
		if part.Text == expected {
			return true
		}
	}
	return false
}

func partsContainTOONMessageText(parts []domain.MessagePart, expected string) bool {
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if text, ok := tooncodec.ExtractMessageText(part.Text); ok && text == expected {
			return true
		}
	}
	return false
}

func countImagesInParts(parts []domain.MessagePart) int {
	total := 0
	for _, part := range parts {
		if part.ImageData != nil {
			total++
		}
	}
	return total
}
