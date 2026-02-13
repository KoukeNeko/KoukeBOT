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
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   "message content for digest testing",
			Timestamp: time.Now(),
		})
	}

	cm := NewContextManager(4000, 800000)
	cm.CompactHistory(conv)

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
}
