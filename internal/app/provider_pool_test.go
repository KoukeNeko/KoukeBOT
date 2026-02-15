package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

type stubProvider struct {
	id    string
	tools []domain.Tool
}

func (p *stubProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: p.id}, nil
}

func (p *stubProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *stubProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *stubProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{ProviderName: p.id}
}

func (p *stubProvider) RegisterTools(tools []domain.Tool) {
	p.tools = append([]domain.Tool(nil), tools...)
}

type errProvider struct {
	err   error
	calls int
}

func (p *errProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls++
	return nil, p.err
}

func (p *errProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.calls++
	return nil, p.err
}

func (p *errProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	p.calls++
	return nil, p.err
}

func (p *errProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{ProviderName: "err-provider"}
}

func (p *errProvider) RegisterTools([]domain.Tool) {}

func TestParseResetDelay(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected time.Duration
	}{
		{
			name:     "JSON format with quotaResetDelay",
			errMsg:   `{"error":{"code":429,"message":"quota exceeded","details":[{"quotaResetDelay":"123s"}]}}`,
			expected: 123 * time.Second,
		},
		{
			name:     "Human-readable: reset after 1h38m34s",
			errMsg:   `You have exhausted your capacity on this model. Your quota will reset after 1h38m34s.`,
			expected: 1*time.Hour + 38*time.Minute + 34*time.Second,
		},
		{
			name:     "Human-readable: reset in 5m",
			errMsg:   `Rate limit exceeded. Your quota will reset in 5m.`,
			expected: 5 * time.Minute,
		},
		{
			name:     "Human-readable: reset after 2h",
			errMsg:   `Quota exceeded. Your quota will reset after 2h.`,
			expected: 2 * time.Hour,
		},
		{
			name:     "No capacity available (transient overload)",
			errMsg:   `No capacity available for model gemini-3-pro-preview on the server`,
			expected: 30 * time.Second,
		},
		{
			name:     "Unparseable error (fallback)",
			errMsg:   `Some unknown error format`,
			expected: 2 * time.Minute,
		},
		{
			name:     "Empty error message (fallback)",
			errMsg:   ``,
			expected: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseResetDelay(tt.errMsg)
			if result != tt.expected {
				t.Errorf("parseResetDelay(%q) = %v, expected %v", tt.errMsg, result, tt.expected)
			}
		})
	}
}

func TestProviderPoolAddOrReplaceByEmail(t *testing.T) {
	pool := NewProviderPool()
	first := &stubProvider{id: "first"}
	second := &stubProvider{id: "second"}

	pool.AddOrReplace(first, "same@example.com")
	pool.AddOrReplace(second, "same@example.com")

	if got := pool.Size(); got != 1 {
		t.Fatalf("pool size = %d, want 1", got)
	}

	p, email, ok := pool.SelectNextProvider()
	if !ok {
		t.Fatal("SelectNextProvider() = not found, want provider")
	}
	if email != "same@example.com" {
		t.Fatalf("selected email = %s, want same@example.com", email)
	}
	if p != second {
		t.Fatalf("selected provider was not replaced instance")
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want errorType
	}{
		{
			name: "quota by status code",
			msg:  "http 429 rate limit exceeded",
			want: errorQuota,
		},
		{
			name: "quota by resource exhausted",
			msg:  "RESOURCE_EXHAUSTED: quota exceeded",
			want: errorQuota,
		},
		{
			name: "validation tool_use_id",
			msg:  "invalid_request_error missing tool_use_id",
			want: errorValidation,
		},
		{
			name: "validation payload",
			msg:  "validation failed: field x",
			want: errorValidation,
		},
		{
			name: "transient 5xx",
			msg:  "HTTP 503 Service Unavailable",
			want: errorTransient,
		},
		{
			name: "transient network",
			msg:  "dial tcp 127.0.0.1:443: connect: connection refused",
			want: errorTransient,
		},
		{
			name: "other errors",
			msg:  "permission denied",
			want: errorOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyError(errors.New(tt.msg)); got != tt.want {
				t.Fatalf("classifyError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestMarkTransientFailureUpdatesPoolStatus(t *testing.T) {
	pool := NewProviderPool()
	pool.AddOrReplace(&stubProvider{id: "provider"}, "a@example.com")

	pool.MarkTransientFailure("a@example.com", errors.New("timeout"), 3*time.Second)
	entries := pool.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries length = %d, want 1", len(entries))
	}
	if entries[0].ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", entries[0].ConsecutiveFailures)
	}
	if entries[0].LastError == "" {
		t.Fatalf("last error should not be empty")
	}
	if entries[0].Status != "exhausted" {
		t.Fatalf("status = %s, want exhausted", entries[0].Status)
	}
}

func TestProviderPoolUsageCountersRolling24h(t *testing.T) {
	pool := NewProviderPool()
	pool.AddOrReplace(&stubProvider{id: "provider"}, "a@example.com")

	now := time.Now()
	pool.recordAttempt("a@example.com", now.Add(-25*time.Hour), true)
	pool.recordAttempt("a@example.com", now, true)
	pool.recordAttempt("a@example.com", now, false)

	entries := pool.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries length = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.AttemptsTotal != 3 {
		t.Fatalf("attempts total = %d, want 3", e.AttemptsTotal)
	}
	if e.Attempts24h != 2 {
		t.Fatalf("attempts 24h = %d, want 2", e.Attempts24h)
	}
	if e.Successes24h != 1 || e.Failures24h != 1 {
		t.Fatalf("24h success/failure = %d/%d, want 1/1", e.Successes24h, e.Failures24h)
	}
	if e.SuccessesTotal != 2 || e.FailuresTotal != 1 {
		t.Fatalf("total success/failure = %d/%d, want 2/1", e.SuccessesTotal, e.FailuresTotal)
	}
}

func TestAddOrReplaceKeepsExistingUsageWhenNewUsageMissing(t *testing.T) {
	pool := NewProviderPool()
	usage := &llm.OAuthUsageStats{
		AttemptsTotal:  5,
		SuccessesTotal: 4,
		FailuresTotal:  1,
	}

	pool.AddOrReplaceWithUsage(&stubProvider{id: "first"}, "a@example.com", usage)
	pool.AddOrReplace(&stubProvider{id: "second"}, "a@example.com")

	entries := pool.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries length = %d, want 1", len(entries))
	}
	if entries[0].AttemptsTotal != 5 {
		t.Fatalf("attempts total = %d, want 5", entries[0].AttemptsTotal)
	}
}

func TestProviderPoolContextDeadlineDoesNotFailoverOrExhaust(t *testing.T) {
	pool := NewProviderPool()
	first := &errProvider{err: context.DeadlineExceeded}
	second := &errProvider{err: nil}
	pool.AddOrReplace(first, "first@example.com")
	pool.AddOrReplace(second, "second@example.com")

	_, err := pool.Chat(context.Background(), llm.ChatRequest{SystemPrompt: "x", Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	if first.calls != 1 {
		t.Fatalf("first provider calls = %d, want 1", first.calls)
	}
	if second.calls != 0 {
		t.Fatalf("second provider should not be called, calls = %d", second.calls)
	}

	entries := pool.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Email == "first@example.com" && e.Status != "available" {
			t.Fatalf("first account status = %s, want available", e.Status)
		}
	}
}

func TestProviderPoolTransientWaitRetrySucceeds(t *testing.T) {
	pool := NewProviderPool()
	pool.SetTransientRecoveryWait(true, 2*time.Second)
	pool.AddOrReplace(&stubProvider{id: "ok"}, "retry@example.com")

	// Simulate short transient cooldown that should recover after wait.
	pool.MarkTransientFailure("retry@example.com", errors.New("timeout"), 80*time.Millisecond)

	start := time.Now()
	resp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Fatalf("expected wait before retry, elapsed=%v", elapsed)
	}
}

func TestProviderPoolTransientWaitDisabledReturnsExhausted(t *testing.T) {
	pool := NewProviderPool()
	pool.SetTransientRecoveryWait(false, 2*time.Second)
	pool.AddOrReplace(&stubProvider{id: "ok"}, "retry@example.com")
	pool.MarkTransientFailure("retry@example.com", errors.New("timeout"), 200*time.Millisecond)

	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("expected exhausted error when transient wait retry is disabled")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("expected exhausted error, got: %v", err)
	}
}
