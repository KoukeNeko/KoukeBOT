package app

import (
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

func TestCompactHistoryAddsDigestAndKeepsRecentMessages(t *testing.T) {
	conv := &domain.Conversation{
		Messages: make([]domain.Message, 0, 32),
	}
	for i := 0; i < 30; i++ {
		role := domain.RoleUser
		content := "message content for digest testing"
		if i%2 == 1 {
			role = domain.RoleAssistant
			content = "請你立刻修正這個愚民級錯誤，別再浪費時間。"
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   content,
			Timestamp: time.Now(),
		})
	}

	cm := NewContextManager(4000, 800000)
	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Variables: map[string]any{
				"insult_list": []any{"愚民", "廢柴"},
			},
		},
	}
	cm.CompactHistory(conv, persona)

	if got := len(conv.Messages); got != 20 {
		t.Fatalf("len(conv.Messages) = %d, want 20", got)
	}
	if len(conv.ContextDigests) == 0 {
		t.Fatalf("context digest should be created")
	}
	lastDigest := conv.ContextDigests[len(conv.ContextDigests)-1]
	if lastDigest.Summary == "" {
		t.Fatalf("digest summary should not be empty")
	}
	if len(lastDigest.StyleQuotes) == 0 {
		t.Fatalf("expected style quotes to be extracted")
	}
	if lastDigest.Attitude == "" {
		t.Fatalf("expected attitude to be classified")
	}
}
