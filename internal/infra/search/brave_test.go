package search

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestBuildCacheKey(t *testing.T) {
	client := NewBraveSearchClient("test-key")

	tests := []struct {
		name     string
		req      SearchRequest
		expected string
	}{
		{
			name: "basic query",
			req: SearchRequest{
				Query: "golang testing",
				Count: 5,
			},
			expected: "golang testing|5|||",
		},
		{
			name: "full parameters",
			req: SearchRequest{
				Query:      "台灣天氣",
				Count:      10,
				Country:    "TW",
				SearchLang: "zh-TW",
				Freshness:  "pd",
			},
			expected: "台灣天氣|10|TW|zh-TW|pd",
		},
		{
			name: "default count",
			req: SearchRequest{
				Query: "test",
			},
			expected: "test|0|||",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.buildCacheKey(tt.req)
			if got != tt.expected {
				t.Errorf("buildCacheKey() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildURL(t *testing.T) {
	client := NewBraveSearchClient("test-key")

	tests := []struct {
		name    string
		req     SearchRequest
		wantErr bool
		checkFn func(string) bool
	}{
		{
			name: "valid basic query",
			req: SearchRequest{
				Query: "golang",
				Count: 5,
			},
			wantErr: false,
			checkFn: func(url string) bool {
				return len(url) > 0 &&
					   contains(url, "q=golang") &&
					   contains(url, "count=5")
			},
		},
		{
			name: "query with spaces",
			req: SearchRequest{
				Query: "hello world",
				Count: 3,
			},
			wantErr: false,
			checkFn: func(url string) bool {
				return contains(url, "hello") && contains(url, "world")
			},
		},
		{
			name: "with country and language",
			req: SearchRequest{
				Query:      "news",
				Count:      5,
				Country:    "TW",
				SearchLang: "zh-TW",
			},
			wantErr: false,
			checkFn: func(url string) bool {
				return contains(url, "country=TW") &&
					   contains(url, "search_lang=zh-TW")
			},
		},
		{
			name: "empty query",
			req: SearchRequest{
				Query: "",
			},
			wantErr: true,
		},
		{
			name: "count too high",
			req: SearchRequest{
				Query: "test",
				Count: 100,
			},
			wantErr: true,
		},
		{
			name: "count negative",
			req: SearchRequest{
				Query: "test",
				Count: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := client.buildURL(tt.req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("buildURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("buildURL() unexpected error: %v", err)
				return
			}

			if tt.checkFn != nil && !tt.checkFn(url) {
				t.Errorf("buildURL() = %v, validation failed", url)
			}
		})
	}
}

func TestCacheOperations(t *testing.T) {
	cache := newSearchCache()

	// Test set and get
	result1 := SearchResult{
		Query:    "test query",
		Provider: "brave",
		Count:    5,
		Results:  []SearchResultItem{},
	}

	cache.set("test|5|||", result1)

	// Should retrieve from cache
	got, found := cache.get("test|5|||")
	if !found {
		t.Error("cache.get() expected to find cached result")
	}
	if got.Query != result1.Query {
		t.Errorf("cache.get() query = %v, want %v", got.Query, result1.Query)
	}

	// Test case-insensitive and trimming
	got2, found2 := cache.get("  TEST|5|||  ")
	if !found2 {
		t.Error("cache.get() should be case-insensitive and trim whitespace")
	}
	if got2.Query != result1.Query {
		t.Errorf("cache.get() with different case = %v, want %v", got2.Query, result1.Query)
	}

	// Test non-existent key
	_, found3 := cache.get("nonexistent")
	if found3 {
		t.Error("cache.get() should not find non-existent key")
	}
}

func TestCacheExpiration(t *testing.T) {
	cache := newSearchCache()
	cache.ttl = 100 * time.Millisecond // Short TTL for testing

	result := SearchResult{
		Query: "expiring query",
	}

	cache.set("expire-test|5|||", result)

	// Should be available immediately
	_, found := cache.get("expire-test|5|||")
	if !found {
		t.Error("cache.get() should find recently cached result")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, found2 := cache.get("expire-test|5|||")
	if found2 {
		t.Error("cache.get() should not find expired result")
	}
}

func TestCacheEviction(t *testing.T) {
	cache := newSearchCache()
	cache.maxSize = 3 // Small size for testing

	// Fill cache
	for i := 0; i < 3; i++ {
		result := SearchResult{Query: "query"}
		cache.set(string(rune('a'+i)), result)
	}

	if len(cache.entries) != 3 {
		t.Errorf("cache size = %d, want 3", len(cache.entries))
	}

	// Add one more - should evict oldest
	result := SearchResult{Query: "new"}
	cache.set("d", result)

	if len(cache.entries) != 3 {
		t.Errorf("cache size after eviction = %d, want 3", len(cache.entries))
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxLen  int
		want    string
	}{
		{
			name:   "short string",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "truncate needed",
			input:  "hello world this is a long string",
			maxLen: 10,
			want:   "hello worl...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 5,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchRequest_DefaultCount(t *testing.T) {
	client := NewBraveSearchClient("test-key")

	req := SearchRequest{
		Query: "test",
		Count: 0, // Should default to 5
	}

	url, err := client.buildURL(req)
	if err != nil {
		t.Fatalf("buildURL() unexpected error: %v", err)
	}

	if !contains(url, "count=5") {
		t.Error("buildURL() should default count to 5")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark for cache operations
func BenchmarkCacheSet(b *testing.B) {
	cache := newSearchCache()
	result := SearchResult{Query: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.set("benchmark|5|||", result)
	}
}

func BenchmarkCacheGet(b *testing.B) {
	cache := newSearchCache()
	result := SearchResult{Query: "test"}
	cache.set("benchmark|5|||", result)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.get("benchmark|5|||")
	}
}

// Integration test context (requires actual API key - skipped in normal testing)
func TestBraveSearch_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test requires BRAVE_API_KEY environment variable
	// Run with: BRAVE_API_KEY=xxx go test -v -run TestBraveSearch_Integration
	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		t.Skip("BRAVE_API_KEY not set, skipping integration test")
	}

	client := NewBraveSearchClient(apiKey)
	ctx := context.Background()

	req := SearchRequest{
		Query: "golang programming",
		Count: 3,
	}

	result, err := client.Search(ctx, req)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	if result.Query != req.Query {
		t.Errorf("Search() query = %v, want %v", result.Query, req.Query)
	}

	if len(result.Results) == 0 {
		t.Error("Search() returned no results")
	}

	if result.Provider != "brave" {
		t.Errorf("Search() provider = %v, want brave", result.Provider)
	}

	// Test cache hit
	result2, err := client.Search(ctx, req)
	if err != nil {
		t.Fatalf("Search() second call error: %v", err)
	}

	if !result2.Cached {
		t.Error("Search() second call should be cached")
	}
}
