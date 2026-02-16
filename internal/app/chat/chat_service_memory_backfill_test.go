package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	tooncodec "github.com/KoukeNeko/KoukeBOT/internal/infra/toon"
)

func TestHandleMessagePreflightBackfillsDigestAndCompactsLegacyHistory(t *testing.T) {
	ctx := context.Background()
	convStore := store.NewMemoryStore()
	channelID := "ch-long"
	personaID := "persona-a"

	conv := &domain.Conversation{
		ID:        "conv-long-1",
		ChannelID: channelID,
		PersonaID: personaID,
		Messages:  make([]domain.Message, 0, 236),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	for i := 0; i < 236; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("historic-%03d", i),
			Timestamp: time.Now().Add(-time.Duration(236-i) * time.Minute),
		})
	}
	if err := convStore.Save(ctx, conv); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	personas := map[string]*domain.PersonaModule{
		personaID: {
			Config: domain.PersonaConfig{
				Meta:           domain.PersonaMeta{ID: personaID, Name: "Test Persona"},
				SystemTemplate: "You are test.\n{{.Lore}}\n{{.Anchors}}\n{{.Time}}",
			},
			Lore: "test lore",
		},
	}
	service := NewChatService(&stubChatProvider{}, NewContextAssembler(), convStore, nil, 0, personas, nil, nil)
	service.UpdateMemoryRetentionPolicy(MemoryRetentionPolicy{RecentRounds: 50})

	resp, err := service.HandleMessage(ctx, channelID, personaID, "latest input")
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatalf("expected non-empty response")
	}

	got, err := convStore.Get(ctx, channelID, personaID)
	if err != nil {
		t.Fatalf("load conversation after handle: %v", err)
	}
	if len(got.ContextDigests) == 0 {
		t.Fatalf("expected digest backfill for long legacy conversation")
	}

	// keep 100 messages from preflight, then append current user + assistant = 102
	if len(got.Messages) != 102 {
		t.Fatalf("messages len = %d, want 102", len(got.Messages))
	}
	firstText, ok := tooncodec.ExtractMessageText(got.Messages[0].Content)
	if !ok {
		t.Fatalf("expected first kept history message to be TOON msg, got: %q", got.Messages[0].Content)
	}
	if firstText != "historic-136" {
		t.Fatalf("unexpected first kept history text: %q", firstText)
	}
	last := got.Messages[len(got.Messages)-1]
	if last.Role != domain.RoleAssistant {
		t.Fatalf("last role = %s, want assistant", last.Role)
	}
}

type stubChatProvider struct{}

func (p *stubChatProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content:    "stub-response",
		TokensUsed: 123,
	}, nil
}

func (p *stubChatProvider) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *stubChatProvider) StreamChatMultimodal(_ context.Context, _ string, _ []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *stubChatProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{
		SupportsThinking:  true,
		ContextWindow:     1_000_000,
		SupportsStreaming: true,
		ProviderName:      "stub",
	}
}

func (p *stubChatProvider) RegisterTools(_ []domain.Tool) {}
