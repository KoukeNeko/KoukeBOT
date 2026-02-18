package provider

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

// switchableErrProvider fails on the primary model but yields a switchable fallback.
type switchableErrProvider struct {
	errProvider
	fallbackID string
}

func (p *switchableErrProvider) WithModel(model string) llm.Provider {
	return &stubProvider{id: p.fallbackID}
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

type countingProvider struct {
	id              string
	chatErr         error
	streamErr       error
	multimodalErr   error
	chatCalls       int
	streamCalls     int
	multimodalCalls int
}

func (p *countingProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	p.chatCalls++
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	return &llm.ChatResponse{Content: p.id}, nil
}

func (p *countingProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.streamCalls++
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	ch := make(chan llm.StreamChunk, 2)
	ch <- llm.StreamChunk{Content: p.id}
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (p *countingProvider) StreamChatMultimodal(context.Context, string, []domain.MultimodalMessage, domain.GenerationConfig) (<-chan llm.StreamChunk, error) {
	p.multimodalCalls++
	if p.multimodalErr != nil {
		return nil, p.multimodalErr
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *countingProvider) ModelInfo() llm.ModelCapabilities {
	return llm.ModelCapabilities{ProviderName: p.id, Model: "gemini-2.5-pro"}
}

func (p *countingProvider) RegisterTools([]domain.Tool) {}

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
			name:     "No capacity available now handled by modelCapacityCooldown",
			errMsg:   `No capacity available for model gemini-3-pro-preview on the server`,
			expected: 2 * time.Minute, // No longer special-cased in parseResetDelay
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
			name: "model capacity: No capacity available",
			msg:  "code assist HTTP 429: No capacity available for model gemini-3-pro-preview on the server",
			want: errorModelCapacity,
		},
		{
			name: "model capacity: MODEL_CAPACITY reason",
			msg:  `429 RESOURCE_EXHAUSTED MODEL_CAPACITY: no available instances`,
			want: errorModelCapacity,
		},
		{
			name: "model capacity: model capacity lowercase",
			msg:  "model capacity exhausted on server",
			want: errorModelCapacity,
		},
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
	if entries[0].Status != "cooldown" {
		t.Fatalf("status = %s, want cooldown", entries[0].Status)
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
	metrics := pool.MetricsSnapshot()
	if metrics.TransientWaitRetry == 0 {
		t.Fatalf("expected transient wait retry metric to increment")
	}
	if metrics.TransientWaitRetrySucceeded == 0 {
		t.Fatalf("expected transient wait retry success metric to increment")
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
	metrics := pool.MetricsSnapshot()
	if metrics.AllAccountsExhausted == 0 {
		t.Fatalf("expected all accounts exhausted metric to increment")
	}
}

func TestProviderPoolQuotaErrorFailsOverToNextAccount(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	first := &errProvider{err: errors.New(`429 RESOURCE_EXHAUSTED {"quotaResetDelay":"10s"}`)}
	second := &stubProvider{id: "ok"}
	pool.AddOrReplace(first, "first@example.com")
	pool.AddOrReplace(second, "second@example.com")

	resp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected failover success, got error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	entries := pool.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Email == "first@example.com" && entry.Status != "cooldown" {
			t.Fatalf("first account status = %s, want cooldown", entry.Status)
		}
	}
}

func TestProviderPoolModelCapacityAppliesGlobalCooldown(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	first := &errProvider{err: errors.New("code assist HTTP 429: No capacity available for model gemini-3-pro-preview")}
	second := &stubProvider{id: "ok"}
	pool.AddOrReplace(first, "first@example.com")
	pool.AddOrReplace(second, "second@example.com")

	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected model capacity error, got nil")
	}
	if !strings.Contains(err.Error(), "No capacity available") {
		t.Fatalf("expected model capacity error, got: %v", err)
	}

	// First should be called exactly once; second should NOT be called.
	if first.calls != 1 {
		t.Fatalf("first provider calls = %d, want 1", first.calls)
	}

	// Both accounts should be in cooldown due to global cooldown.
	entries := pool.Entries()
	for _, e := range entries {
		if e.Status != "cooldown" {
			t.Fatalf("account %s status = %s, want cooldown", e.Email, e.Status)
		}
	}

	metrics := pool.MetricsSnapshot()
	if metrics.ModelCapacityGlobalCooldown == 0 {
		t.Fatal("expected model capacity global cooldown metric to increment")
	}
}

func TestProviderPoolFallbackModelSucceeds(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	pool.SetFallbackModel("gemini-2.0-flash")

	capErr := errors.New("code assist HTTP 429: No capacity available for model gemini-3-pro-preview")
	pool.AddOrReplace(&switchableErrProvider{
		errProvider: errProvider{err: capErr},
		fallbackID:  "fallback-ok",
	}, "a@example.com")

	resp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if resp == nil || !strings.Contains(resp.Content, "fallback-ok") {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if !strings.Contains(resp.Content, "[Model fallback] switched to gemini-2.0-flash") {
		t.Fatalf("expected fallback notice in response, got: %q", resp.Content)
	}
}

func TestProviderPoolFallbackModelDisabledReturnsError(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	// No fallback model set.

	capErr := errors.New("code assist HTTP 429: No capacity available for model gemini-3-pro-preview")
	pool.AddOrReplace(&errProvider{err: capErr}, "a@example.com")

	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when fallback is not configured")
	}
	if !strings.Contains(err.Error(), "No capacity available") {
		t.Fatalf("expected model capacity error, got: %v", err)
	}
}

func TestRequestQueueEnqueueAndDrain(t *testing.T) {
	q := newRequestQueue(2, 2*time.Second)

	// Enqueue a request, then signal it.
	done := make(chan error, 1)
	go func() {
		_, err := q.enqueue(context.Background())
		done <- err
	}()

	time.Sleep(20 * time.Millisecond) // Let goroutine settle.
	if q.depth() != 1 {
		t.Fatalf("queue depth = %d, want 1", q.depth())
	}

	q.notifyAll()
	if err := <-done; err != nil {
		t.Fatalf("expected nil after notify, got: %v", err)
	}
}

func TestRequestQueueFull(t *testing.T) {
	q := newRequestQueue(1, 2*time.Second)

	// Fill the queue.
	go func() {
		q.enqueue(context.Background()) //nolint:errcheck
	}()
	time.Sleep(20 * time.Millisecond)

	// Second enqueue should fail immediately.
	_, err := q.enqueue(context.Background())
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got: %v", err)
	}
}

func TestRequestQueueTimeout(t *testing.T) {
	q := newRequestQueue(5, 50*time.Millisecond)

	_, err := q.enqueue(context.Background())
	if !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("expected ErrQueueTimeout, got: %v", err)
	}
}

func TestRequestQueueContextCancellation(t *testing.T) {
	q := newRequestQueue(5, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := q.enqueue(ctx)
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestCircuitBreakerStateMachine(t *testing.T) {
	cb := newCircuitBreaker(3, 50*time.Millisecond)

	// Closed: allows requests.
	if !cb.allowRequest() {
		t.Fatal("closed circuit should allow requests")
	}

	// Record failures below threshold.
	cb.recordFailure()
	cb.recordFailure()
	if !cb.allowRequest() {
		t.Fatal("should still allow with 2 failures (threshold=3)")
	}

	// Third failure trips the circuit open.
	cb.recordFailure()
	if cb.allowRequest() {
		t.Fatal("open circuit should reject requests")
	}

	// Wait for open duration to expire → half-open allows one probe.
	time.Sleep(60 * time.Millisecond)
	if !cb.allowRequest() {
		t.Fatal("half-open should allow one probe after open duration")
	}

	// While half-open, concurrent requests are rejected.
	if cb.allowRequest() {
		t.Fatal("half-open should reject second concurrent request")
	}

	// Probe success → closes circuit.
	cb.recordSuccess()
	if !cb.allowRequest() {
		t.Fatal("circuit should be closed after success")
	}
}

func TestCircuitBreakerReOpensOnProbeFailure(t *testing.T) {
	cb := newCircuitBreaker(1, 50*time.Millisecond)

	cb.recordFailure() // Opens immediately (threshold=1).
	if cb.allowRequest() {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond)
	if !cb.allowRequest() {
		t.Fatal("half-open should allow probe")
	}

	// Probe fails → back to open.
	cb.recordFailure()
	if cb.allowRequest() {
		t.Fatal("should be open again after probe failure")
	}
}

func TestProviderPoolCircuitBreakerRejectsWhenOpen(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	pool.SetCircuitBreaker(1, 5*time.Second)

	capErr := errors.New("code assist HTTP 429: No capacity available for model gemini-3-pro-preview")
	pool.AddOrReplace(&errProvider{err: capErr}, "a@example.com")

	// First call: triggers model capacity → circuit records failure → opens.
	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Second call: circuit breaker should reject immediately.
	_, err = pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestProviderPoolSetCircuitBreakerDisable(t *testing.T) {
	pool := NewProviderPool()
	pool.SetCircuitBreaker(1, time.Second)
	if pool.circuitBreaker == nil {
		t.Fatal("expected circuit breaker to be enabled")
	}

	pool.SetCircuitBreaker(0, 0)
	if pool.circuitBreaker != nil {
		t.Fatal("expected circuit breaker to be disabled")
	}
}

func TestProviderPoolTransientErrorFailsOverToNextAccount(t *testing.T) {
	pool := NewProviderPool()
	first := &errProvider{err: errors.New("HTTP 503 Service Unavailable")}
	second := &stubProvider{id: "ok"}
	pool.AddOrReplace(first, "first@example.com")
	pool.AddOrReplace(second, "second@example.com")

	resp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected failover success, got error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	entries := pool.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Email != "first@example.com" {
			continue
		}
		if entry.ConsecutiveFailures == 0 {
			t.Fatalf("expected first account consecutive failures > 0")
		}
	}
}

func TestProviderPoolOAuthExhaustionFallbackOnQuotaExhausted(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	quotaErr := errors.New(`429 RESOURCE_EXHAUSTED {"quotaResetDelay":"90s"}`)
	pool.AddOrReplace(&errProvider{err: quotaErr}, "quota@example.com")

	fallback := &countingProvider{id: "aistudio-rpc"}
	pool.SetOAuthExhaustionFallback(fallback)

	resp, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected AI Studio fallback success, got error: %v", err)
	}
	if resp == nil || !strings.Contains(resp.Content, "aistudio-rpc") {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if !strings.Contains(resp.Content, "[Provider fallback]") {
		t.Fatalf("expected provider fallback annotation, got: %q", resp.Content)
	}
	if fallback.chatCalls != 1 {
		t.Fatalf("fallback chat calls = %d, want 1", fallback.chatCalls)
	}

	metrics := pool.MetricsSnapshot()
	if metrics.OAuthExhaustionFallbackTry != 1 || metrics.OAuthExhaustionFallbackOK != 1 || metrics.OAuthExhaustionFallbackFail != 0 {
		t.Fatalf("unexpected fallback metrics: %+v", metrics)
	}
}

func TestProviderPoolOAuthExhaustionFallbackSkipsNonQuotaErrors(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	pool.AddOrReplace(&errProvider{err: errors.New("validation failed: missing tool_use_id")}, "validation@example.com")

	fallback := &countingProvider{id: "aistudio-rpc"}
	pool.SetOAuthExhaustionFallback(fallback)

	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "validation") {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if fallback.chatCalls != 0 {
		t.Fatalf("fallback should not be called, calls=%d", fallback.chatCalls)
	}
	if metrics := pool.MetricsSnapshot(); metrics.OAuthExhaustionFallbackTry != 0 {
		t.Fatalf("unexpected fallback attempt metric: %+v", metrics)
	}
}

func TestProviderPoolOAuthExhaustionFallbackDisabledForMultimodal(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	quotaErr := errors.New(`429 RESOURCE_EXHAUSTED {"quotaResetDelay":"90s"}`)
	pool.AddOrReplace(&errProvider{err: quotaErr}, "quota@example.com")

	fallback := &countingProvider{id: "aistudio-rpc"}
	pool.SetOAuthExhaustionFallback(fallback)

	_, err := pool.StreamChatMultimodal(
		context.Background(),
		"sys",
		[]domain.MultimodalMessage{{Role: domain.RoleUser, Parts: []domain.MessagePart{{Text: "hi"}}}},
		domain.GenerationConfig{},
	)
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("expected exhausted error, got: %v", err)
	}
	if fallback.chatCalls != 0 || fallback.streamCalls != 0 || fallback.multimodalCalls != 0 {
		t.Fatalf("fallback should not be called on multimodal path: %#v", fallback)
	}
	if metrics := pool.MetricsSnapshot(); metrics.OAuthExhaustionFallbackTry != 0 {
		t.Fatalf("unexpected fallback attempt metric: %+v", metrics)
	}
}

func TestProviderPoolOAuthExhaustionFallbackFailureKeepsExhaustedSemantics(t *testing.T) {
	pool := NewProviderPool()
	pool.SetAutoRetryCount(0)
	quotaErr := errors.New(`429 RESOURCE_EXHAUSTED {"quotaResetDelay":"90s"}`)
	pool.AddOrReplace(&errProvider{err: quotaErr}, "quota@example.com")

	fallback := &countingProvider{
		id:      "aistudio-rpc",
		chatErr: errors.New("aistudio rpc HTTP 401"),
	}
	pool.SetOAuthExhaustionFallback(fallback)

	_, err := pool.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "x",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected exhausted error with fallback context")
	}
	if !strings.Contains(err.Error(), "all 1 OAuth account(s) exhausted") {
		t.Fatalf("expected exhausted error semantics, got: %v", err)
	}
	if !strings.Contains(err.Error(), "AI Studio fallback failed") {
		t.Fatalf("expected fallback failure context, got: %v", err)
	}
	if fallback.chatCalls != 1 {
		t.Fatalf("fallback chat calls = %d, want 1", fallback.chatCalls)
	}

	metrics := pool.MetricsSnapshot()
	if metrics.OAuthExhaustionFallbackTry != 1 || metrics.OAuthExhaustionFallbackOK != 0 || metrics.OAuthExhaustionFallbackFail != 1 {
		t.Fatalf("unexpected fallback metrics: %+v", metrics)
	}
}
