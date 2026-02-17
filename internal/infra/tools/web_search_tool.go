package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
)

// WebSearchTool implements domain.Tool for web search capability (OpenClaw 相容：支援多提供商)
type WebSearchTool struct {
	searchProvider search.SearchProvider
}

// NewWebSearchTool creates a new web search tool
func NewWebSearchTool(provider search.SearchProvider) *WebSearchTool {
	return &WebSearchTool{
		searchProvider: provider,
	}
}

// Name returns the tool identifier
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description returns the tool description for the LLM
func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Use this when you need up-to-date facts, news, or information not in your training data. The tool returns a list of relevant web pages with titles, URLs, and descriptions."
}

// InputSchema returns the JSON Schema for tool parameters
func (t *WebSearchTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query string. Be specific and use keywords that would appear in relevant results.",
			},
			"count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return (1-10). Default is 5.",
				"minimum":     1,
				"maximum":     10,
			},
			"country": map[string]interface{}{
				"type":        "string",
				"description": "Country code for localized results (e.g., 'US', 'TW', 'JP', 'ALL'). Optional.",
			},
			"search_lang": map[string]interface{}{
				"type":        "string",
				"description": "Search language code (e.g., 'en', 'zh-TW', 'ja'). Optional.",
			},
			"freshness": map[string]interface{}{
				"type":        "string",
				"description": "Time filter: 'pd' (past day), 'pw' (past week), 'pm' (past month), 'py' (past year). Optional.",
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs the web search and returns formatted results
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	req, err := t.parseInput(input)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Invalid input: %v", err),
			IsError: true,
		}, nil
	}

	logTool.Logf("web_search: q=%q", req.Query)
	result, err := t.searchProvider.Search(ctx, req)
	if err != nil {
		logTool.Warnf("web_search failed: %v", err)
		return domain.ToolResult{
			Content: fmt.Sprintf("Search failed: %v", err),
			IsError: true,
		}, nil
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Failed to format results: %v", err),
			IsError: true,
		}, nil
	}

	wrappedContent := wrapWebContent(string(resultJSON), result.Cached)

	return domain.ToolResult{
		Content: wrappedContent,
		IsError: false,
	}, nil
}

// parseInput extracts and validates search parameters from tool input
func (t *WebSearchTool) parseInput(input map[string]interface{}) (search.SearchRequest, error) {
	req := search.SearchRequest{}

	query, ok := input["query"].(string)
	if !ok || query == "" {
		return req, fmt.Errorf("query is required and must be a string")
	}
	req.Query = query

	if count, ok := input["count"].(float64); ok {
		req.Count = int(count)
	}
	if country, ok := input["country"].(string); ok {
		req.Country = country
	}
	if searchLang, ok := input["search_lang"].(string); ok {
		req.SearchLang = searchLang
	}
	if freshness, ok := input["freshness"].(string); ok {
		req.Freshness = freshness
	}

	return req, nil
}

// wrapWebContent wraps search results to indicate external source (OpenClaw pattern)
func wrapWebContent(content string, cached bool) string {
	header := "=== WEB SEARCH RESULTS ==="
	if cached {
		header = "=== WEB SEARCH RESULTS (CACHED) ==="
	}

	footer := "=== END SEARCH RESULTS ==="

	return fmt.Sprintf("%s\n\n%s\n\n%s\n\nNote: These results are from an external web search. Verify important facts and cite sources when using this information.", header, content, footer)
}
