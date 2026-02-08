package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
)

var logTool = logger.New("tools", logger.Green)

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
	// Parse input parameters
	req, err := t.parseInput(input)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Invalid input: %v", err),
			IsError: true,
		}, nil // Return error as tool result, not execution error
	}

	// Execute search
	logTool.Logf("web_search: q=%q", req.Query)
	result, err := t.searchProvider.Search(ctx, req)
	if err != nil {
		logTool.Warnf("web_search failed: %v", err)
		return domain.ToolResult{
			Content: fmt.Sprintf("Search failed: %v", err),
			IsError: true,
		}, nil
	}

	// Format result as JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Failed to format results: %v", err),
			IsError: true,
		}, nil
	}

	// Wrap content to indicate it's from external source (OpenClaw style)
	wrappedContent := wrapWebContent(string(resultJSON), result.Cached)

	return domain.ToolResult{
		Content: wrappedContent,
		IsError: false,
	}, nil
}

// parseInput extracts and validates search parameters from tool input
func (t *WebSearchTool) parseInput(input map[string]interface{}) (search.SearchRequest, error) {
	req := search.SearchRequest{}

	// Query (required)
	query, ok := input["query"].(string)
	if !ok || query == "" {
		return req, fmt.Errorf("query is required and must be a string")
	}
	req.Query = query

	// Count (optional)
	if count, ok := input["count"].(float64); ok {
		req.Count = int(count)
	}

	// Country (optional)
	if country, ok := input["country"].(string); ok {
		req.Country = country
	}

	// SearchLang (optional)
	if searchLang, ok := input["search_lang"].(string); ok {
		req.SearchLang = searchLang
	}

	// Freshness (optional)
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

// WebFetchTool implements domain.Tool for web page fetching capability
type WebFetchTool struct {
	fetcher *search.WebFetcher
}

// NewWebFetchTool creates a new web fetch tool
func NewWebFetchTool(fetcher *search.WebFetcher) *WebFetchTool {
	return &WebFetchTool{
		fetcher: fetcher,
	}
}

// Name returns the tool identifier
func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

// Description returns the tool description for the LLM
func (t *WebFetchTool) Description() string {
	return "Fetch and extract readable content from a web page. Returns the page content in clean Markdown format. Use this when you need to read the full content of a specific URL. The tool extracts the main article/content, removing navigation, ads, and other clutter."
}

// InputSchema returns the JSON Schema for tool parameters
func (t *WebFetchTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL of the web page to fetch (required). Must be a valid HTTP/HTTPS URL.",
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters to return (default 20000, range 1000-50000). Content will be truncated if it exceeds this limit.",
				"minimum":     1000,
				"maximum":     50000,
			},
		},
		"required": []string{"url"},
	}
}

// Execute performs the web fetch and returns formatted results
func (t *WebFetchTool) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	// Parse input parameters
	req, err := t.parseInput(input)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Invalid input: %v", err),
			IsError: true,
		}, nil
	}

	// Execute fetch
	logTool.Logf("web_fetch: url=%s", req.URL)
	result, err := t.fetcher.Fetch(ctx, req)
	if err != nil {
		logTool.Warnf("web_fetch failed: %v", err)
		return domain.ToolResult{
			Content: fmt.Sprintf("Fetch failed: %v", err),
			IsError: true,
		}, nil
	}

	// Format result as JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Failed to format results: %v", err),
			IsError: true,
		}, nil
	}

	// Wrap content to indicate it's from external source (OpenClaw style)
	wrappedContent := wrapWebFetchContent(string(resultJSON), result.Truncated)

	return domain.ToolResult{
		Content: wrappedContent,
		IsError: false,
	}, nil
}

// parseInput extracts and validates fetch parameters from tool input
func (t *WebFetchTool) parseInput(input map[string]interface{}) (search.FetchRequest, error) {
	req := search.FetchRequest{}

	// URL (required)
	url, ok := input["url"].(string)
	if !ok || url == "" {
		return req, fmt.Errorf("url is required and must be a string")
	}
	req.URL = url

	// MaxChars (optional)
	if maxChars, ok := input["max_chars"].(float64); ok {
		req.MaxChars = int(maxChars)
	}

	return req, nil
}

// wrapWebFetchContent wraps fetch results to indicate external source (OpenClaw pattern)
func wrapWebFetchContent(content string, truncated bool) string {
	header := "=== WEB FETCH RESULT ==="
	if truncated {
		header = "=== WEB FETCH RESULT (TRUNCATED) ==="
	}

	footer := "=== END FETCH RESULT ==="

	return fmt.Sprintf("%s\n\n%s\n\n%s\n\nNote: This content was fetched from an external web page and converted to Markdown. Always verify information and cite the source URL when using this content.", header, content, footer)
}
