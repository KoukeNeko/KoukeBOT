package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// stubProvider is a minimal LLM provider for summarizer tests.
type stubProvider struct {
	chatResponse *llm.ChatResponse
	chatError    error
}

func (s *stubProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return s.chatResponse, s.chatError
}

func (s *stubProvider) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubProvider) StreamChatMultimodal(_ context.Context, _ string, _ []domain.MultimodalMessage, _ domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{ContextWindow: 200000}
}

func (s *stubProvider) RegisterTools(_ []domain.Tool) {}

func TestSummarizeConversationBlock_ParsesLLMResponse(t *testing.T) {
	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: `{
				"summary": "User discussed project setup and decided on Go modules.",
				"facts": ["User works on KoukeBOT project"],
				"preferences": ["Prefers Go over Python"],
				"open_items": ["Need to add CI pipeline"],
				"decisions": ["Use Go modules for dependency management"],
				"keywords": ["go", "modules", "koukeBOT", "setup"],
				"attitude": "directive",
				"emotional_tone": "composed",
				"style_quotes": ["Just get it done already."]
			}`,
		},
	}

	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "Let's set up the project with Go modules", Timestamp: time.Now()},
		{Role: domain.RoleAssistant, Content: "Just get it done already. Use go mod init.", Timestamp: time.Now()},
	}

	digest, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: messages,
	})
	if err != nil {
		t.Fatalf("SummarizeConversationBlock() error = %v", err)
	}

	if digest.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(digest.Summary, "Go modules") {
		t.Fatalf("summary should mention Go modules, got: %q", digest.Summary)
	}
	if len(digest.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(digest.Facts))
	}
	if digest.Attitude != "directive" {
		t.Fatalf("expected attitude=directive, got %q", digest.Attitude)
	}
	if digest.EmotionalTone != "composed" {
		t.Fatalf("expected tone=composed, got %q", digest.EmotionalTone)
	}
	if len(digest.StyleQuotes) != 1 {
		t.Fatalf("expected 1 style quote, got %d", len(digest.StyleQuotes))
	}
	if digest.ID == "" {
		t.Fatal("digest ID should not be empty")
	}
}

func TestSummarizeConversationBlock_HandlesMarkdownFences(t *testing.T) {
	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: "```json\n{\"summary\": \"test summary\", \"facts\": [], \"preferences\": [], \"open_items\": [], \"decisions\": [], \"keywords\": [\"test\"], \"attitude\": \"\", \"emotional_tone\": \"\", \"style_quotes\": []}\n```",
		},
	}

	digest, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Summary != "test summary" {
		t.Fatalf("expected 'test summary', got %q", digest.Summary)
	}
}

func TestSummarizeConversationBlock_FallsBackOnLLMError(t *testing.T) {
	provider := &stubProvider{
		chatError: fmt.Errorf("provider unavailable"),
	}

	_, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err == nil {
		t.Fatal("expected error when provider fails")
	}
	if !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected provider error in message, got: %v", err)
	}
}

func TestSummarizeConversationBlock_RejectsEmptySummary(t *testing.T) {
	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: `{"summary": "", "facts": [], "preferences": [], "open_items": [], "decisions": [], "keywords": [], "attitude": "", "emotional_tone": "", "style_quotes": []}`,
		},
	}

	_, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestSummarizeConversationBlock_RejectsEmptyMessages(t *testing.T) {
	provider := &stubProvider{}

	_, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: nil,
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestSummarizeConversationBlock_EnforcesFieldLimits(t *testing.T) {
	// LLM returns more than allowed limits
	manyItems := make([]string, 20)
	for i := range manyItems {
		manyItems[i] = fmt.Sprintf("item-%d", i)
	}
	manyJSON := `"` + strings.Join(manyItems, `","`) + `"`

	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: fmt.Sprintf(`{
				"summary": "test",
				"facts": [%s],
				"preferences": [%s],
				"open_items": [%s],
				"decisions": [%s],
				"keywords": [%s],
				"attitude": "critical",
				"emotional_tone": "cold",
				"style_quotes": [%s]
			}`, manyJSON, manyJSON, manyJSON, manyJSON, manyJSON, manyJSON),
		},
	}

	digest, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(digest.Facts) > 8 {
		t.Fatalf("facts should be capped at 8, got %d", len(digest.Facts))
	}
	if len(digest.Preferences) > 8 {
		t.Fatalf("preferences should be capped at 8, got %d", len(digest.Preferences))
	}
	if len(digest.OpenItems) > 10 {
		t.Fatalf("open_items should be capped at 10, got %d", len(digest.OpenItems))
	}
	if len(digest.Decisions) > 10 {
		t.Fatalf("decisions should be capped at 10, got %d", len(digest.Decisions))
	}
	if len(digest.Keywords) > 16 {
		t.Fatalf("keywords should be capped at 16, got %d", len(digest.Keywords))
	}
	if len(digest.StyleQuotes) > 3 {
		t.Fatalf("style_quotes should be capped at 3, got %d", len(digest.StyleQuotes))
	}
}

func TestSummarizeConversationBlock_ValidatesEnumValues(t *testing.T) {
	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: `{"summary": "test", "facts": [], "preferences": [], "open_items": [], "decisions": [], "keywords": [], "attitude": "INVALID_ATTITUDE", "emotional_tone": "unknown_tone", "style_quotes": []}`,
		},
	}

	digest, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Attitude != "" {
		t.Fatalf("invalid attitude should be normalized to empty, got %q", digest.Attitude)
	}
	if digest.EmotionalTone != "" {
		t.Fatalf("invalid emotional_tone should be normalized to empty, got %q", digest.EmotionalTone)
	}
}

func TestSummarizeConversationBlock_RejectsInvalidJSON(t *testing.T) {
	provider := &stubProvider{
		chatResponse: &llm.ChatResponse{
			Content: "This is not JSON at all",
		},
	}

	_, err := SummarizeConversationBlock(context.Background(), provider, SummarizeRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello", Timestamp: time.Now()}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseLLMDigestResponse_CleanJSON(t *testing.T) {
	raw := `{"summary":"User asked about deploy","facts":["runs on k8s"],"preferences":[],"open_items":[],"decisions":[],"keywords":["deploy","k8s"],"attitude":"directive","emotional_tone":"cold","style_quotes":["Fix it now."]}`
	digest, err := parseLLMDigestResponse(raw)
	if err != nil {
		t.Fatalf("parseLLMDigestResponse() error = %v", err)
	}
	if digest.Summary != "User asked about deploy" {
		t.Fatalf("unexpected summary: %q", digest.Summary)
	}
	if digest.Attitude != "directive" {
		t.Fatalf("unexpected attitude: %q", digest.Attitude)
	}
}

func TestFormatMessagesForSummary(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "hello world"},
		{Role: domain.RoleAssistant, Content: "hi there"},
		{Role: domain.RoleUser, Content: ""},
	}

	got := formatMessagesForSummary(messages)
	if !strings.Contains(got, "[User] hello world") {
		t.Fatalf("expected user message formatted, got: %s", got)
	}
	if !strings.Contains(got, "[Assistant] hi there") {
		t.Fatalf("expected assistant message formatted, got: %s", got)
	}
	// Empty message should be skipped
	count := strings.Count(got, "[User]")
	if count != 1 {
		t.Fatalf("expected 1 user tag (empty skipped), got %d", count)
	}
}

func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain json", `{"key":"value"}`, `{"key":"value"}`},
		{"with json fence", "```json\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"with bare fence", "```\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"with whitespace", "  \n{\"key\":\"value\"}\n  ", `{"key":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSONResponse(tt.input)
			if got != tt.want {
				t.Fatalf("cleanJSONResponse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateEnum(t *testing.T) {
	if got := validateEnum("critical", "critical", "directive"); got != "critical" {
		t.Fatalf("expected 'critical', got %q", got)
	}
	if got := validateEnum("CRITICAL", "critical", "directive"); got != "critical" {
		t.Fatalf("expected case-insensitive match 'critical', got %q", got)
	}
	if got := validateEnum("invalid", "critical", "directive"); got != "" {
		t.Fatalf("expected empty for invalid enum, got %q", got)
	}
	if got := validateEnum("", "critical"); got != "" {
		t.Fatalf("expected empty for empty input, got %q", got)
	}
}

func TestCompactHistoryWithDigest_UsesPreBuiltDigest(t *testing.T) {
	conv := &domain.Conversation{
		Messages: make([]domain.Message, 0, 60),
	}
	for i := 0; i < 50; i++ {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		conv.Messages = append(conv.Messages, domain.Message{
			Role:      role,
			Content:   fmt.Sprintf("msg-%02d", i),
			Timestamp: time.Now(),
		})
	}

	preBuilt := &domain.ConversationDigest{
		ID:            "llm-digest-test",
		Summary:       "LLM generated summary about project setup",
		Facts:         []string{"User uses Go"},
		Attitude:      "directive",
		EmotionalTone: "composed",
		StyleQuotes:   []string{"Fix it now."},
		CreatedAt:     time.Now(),
	}

	removedCount, addedDigests := compactHistoryWithDigest(conv, nil, 20, preBuilt)
	if removedCount == 0 {
		t.Fatal("expected messages to be removed")
	}
	if addedDigests != 1 {
		t.Fatalf("expected 1 digest added, got %d", addedDigests)
	}
	if len(conv.ContextDigests) != 1 {
		t.Fatalf("expected 1 digest, got %d", len(conv.ContextDigests))
	}
	if conv.ContextDigests[0].Summary != "LLM generated summary about project setup" {
		t.Fatalf("expected LLM summary, got %q", conv.ContextDigests[0].Summary)
	}
}

func TestCompactHistoryWithDigest_FallsBackToRuleBased(t *testing.T) {
	conv := &domain.Conversation{
		Messages: make([]domain.Message, 0, 60),
	}
	for i := 0; i < 50; i++ {
		role := domain.RoleUser
		content := "我叫小明，我需要幫忙"
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

	persona := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Variables: map[string]any{
				"insult_list": []any{"愚民", "廢柴"},
			},
		},
	}

	// nil preBuiltDigest → rule-based fallback
	removedCount, addedDigests := compactHistoryWithDigest(conv, persona, 20, nil)
	if removedCount == 0 {
		t.Fatal("expected messages to be removed")
	}
	if addedDigests != 1 {
		t.Fatalf("expected 1 digest (rule-based), got %d", addedDigests)
	}
	if conv.ContextDigests[0].Summary == "" {
		t.Fatal("rule-based digest summary should not be empty")
	}
}
