package llm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
)

func TestWebSearchTool_Name(t *testing.T) {
	tool := NewWebSearchTool(search.NewBraveSearchClient("test-key"))

	if tool.Name() != "web_search" {
		t.Errorf("Name() = %v, want web_search", tool.Name())
	}
}

func TestWebSearchTool_Description(t *testing.T) {
	tool := NewWebSearchTool(search.NewBraveSearchClient("test-key"))

	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}

	// Should mention web search and current information
	if !strings.Contains(desc, "web") && !strings.Contains(desc, "search") {
		t.Error("Description() should mention web search")
	}
}

func TestWebSearchTool_InputSchema(t *testing.T) {
	tool := NewWebSearchTool(search.NewBraveSearchClient("test-key"))

	schema := tool.InputSchema()

	// Check type
	if schema["type"] != "object" {
		t.Errorf("InputSchema type = %v, want object", schema["type"])
	}

	// Check properties
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("InputSchema properties should be a map")
	}

	// Query should be required
	if _, hasQuery := props["query"]; !hasQuery {
		t.Error("InputSchema should have query property")
	}

	// Check required fields
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("InputSchema required should be a string slice")
	}

	hasQueryRequired := false
	for _, field := range required {
		if field == "query" {
			hasQueryRequired = true
			break
		}
	}
	if !hasQueryRequired {
		t.Error("InputSchema should require query field")
	}
}

func TestWebSearchTool_ParseInput(t *testing.T) {
	tool := NewWebSearchTool(search.NewBraveSearchClient("test-key"))

	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr bool
		check   func(search.SearchRequest) bool
	}{
		{
			name: "valid basic query",
			input: map[string]interface{}{
				"query": "golang testing",
			},
			wantErr: false,
			check: func(req search.SearchRequest) bool {
				return req.Query == "golang testing"
			},
		},
		{
			name: "with count",
			input: map[string]interface{}{
				"query": "test",
				"count": float64(10),
			},
			wantErr: false,
			check: func(req search.SearchRequest) bool {
				return req.Query == "test" && req.Count == 10
			},
		},
		{
			name: "with all parameters",
			input: map[string]interface{}{
				"query":       "台灣新聞",
				"count":       float64(5),
				"country":     "TW",
				"search_lang": "zh-TW",
				"freshness":   "pd",
			},
			wantErr: false,
			check: func(req search.SearchRequest) bool {
				return req.Query == "台灣新聞" &&
					req.Count == 5 &&
					req.Country == "TW" &&
					req.SearchLang == "zh-TW" &&
					req.Freshness == "pd"
			},
		},
		{
			name:    "missing query",
			input:   map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty query",
			input: map[string]interface{}{
				"query": "",
			},
			wantErr: true,
		},
		{
			name: "invalid query type",
			input: map[string]interface{}{
				"query": 123,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := tool.parseInput(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("parseInput() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parseInput() unexpected error: %v", err)
				return
			}

			if tt.check != nil && !tt.check(req) {
				t.Errorf("parseInput() result failed validation: %+v", req)
			}
		})
	}
}

func TestWebSearchTool_Execute_InvalidInput(t *testing.T) {
	tool := NewWebSearchTool(search.NewBraveSearchClient("test-key"))
	ctx := context.Background()

	// Test with missing query
	result, err := tool.Execute(ctx, map[string]interface{}{})

	// Execute should not return error - errors are in the result
	if err != nil {
		t.Errorf("Execute() should not return error, got: %v", err)
	}

	if !result.IsError {
		t.Error("Execute() result should indicate error")
	}

	if !strings.Contains(result.Content, "Invalid input") {
		t.Errorf("Execute() error content should mention invalid input, got: %v", result.Content)
	}
}

func TestWebSearchTool_WrapWebContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		cached  bool
		checks  []string
	}{
		{
			name:    "normal result",
			content: `{"results":[{"title":"Test","url":"https://example.com"}]}`,
			cached:  false,
			checks: []string{
				"=== WEB SEARCH RESULTS ===",
				"=== END SEARCH RESULTS ===",
				"external web search",
				`{"results":[{"title":"Test","url":"https://example.com"}]}`,
			},
		},
		{
			name:    "cached result",
			content: `{"results":[]}`,
			cached:  true,
			checks: []string{
				"=== WEB SEARCH RESULTS (CACHED) ===",
				"=== END SEARCH RESULTS ===",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapWebContent(tt.content, tt.cached)

			for _, check := range tt.checks {
				if !strings.Contains(wrapped, check) {
					t.Errorf("wrapWebContent() should contain %q, got:\n%s", check, wrapped)
				}
			}
		})
	}
}

func TestWebSearchTool_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		t.Skip("BRAVE_API_KEY not set, skipping integration test")
	}

	client := search.NewBraveSearchClient(apiKey)
	tool := NewWebSearchTool(client)
	ctx := context.Background()

	input := map[string]interface{}{
		"query": "golang",
		"count": float64(3),
	}

	result, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if result.IsError {
		t.Fatalf("Execute() returned error result: %v", result.Content)
	}

	// Check content structure
	if !strings.Contains(result.Content, "=== WEB SEARCH RESULTS ===") {
		t.Error("Execute() result should contain header")
	}

	if !strings.Contains(result.Content, "=== END SEARCH RESULTS ===") {
		t.Error("Execute() result should contain footer")
	}

	// Try to parse the JSON portion
	lines := strings.Split(result.Content, "\n")
	var jsonLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			jsonLine = line
			break
		}
	}

	if jsonLine == "" {
		t.Fatal("Execute() result should contain JSON data")
	}

	var searchResult search.SearchResult
	if err := json.Unmarshal([]byte(jsonLine), &searchResult); err != nil {
		t.Errorf("Execute() result JSON parse error: %v", err)
	}

	if searchResult.Query != "golang" {
		t.Errorf("Execute() result query = %v, want golang", searchResult.Query)
	}

	if len(searchResult.Results) == 0 {
		t.Error("Execute() result should have search results")
	}

	// Test cache hit
	result2, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute() second call error: %v", err)
	}

	if !strings.Contains(result2.Content, "CACHED") {
		t.Error("Execute() second call should return cached result")
	}
}

// Test that tool conforms to domain.Tool interface
func TestWebSearchTool_Interface(t *testing.T) {
	var _ domain.Tool = (*WebSearchTool)(nil)
}
