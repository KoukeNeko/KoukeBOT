package tools

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/search"
)

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
	req, err := t.parseInput(input)
	if err != nil {
		return domain.ToolResult{
			Content: fmt.Sprintf("Invalid input: %v", err),
			IsError: true,
		}, nil
	}

	logTool.Logf("web_fetch: url=%s", req.URL)
	result, err := t.fetcher.Fetch(ctx, req)
	if err != nil {
		logTool.Warnf("web_fetch failed: %v", err)
		return domain.ToolResult{
			Content: fmt.Sprintf("Fetch failed: %v", err),
			IsError: true,
		}, nil
	}

	formattedResult := fmt.Sprintf("URL: %s\nTitle: %s\nExtract Mode: %s\nLength: %d characters\nTruncated: %v\n\n--- CONTENT START ---\n\n%s\n\n--- CONTENT END ---",
		result.URL,
		result.Title,
		result.ExtractMode,
		result.TextLength,
		result.Truncated,
		result.Content,
	)

	wrappedContent := wrapWebFetchContent(formattedResult, result.Truncated)

	return domain.ToolResult{
		Content: wrappedContent,
		IsError: false,
	}, nil
}

// parseInput extracts and validates fetch parameters from tool input
func (t *WebFetchTool) parseInput(input map[string]interface{}) (search.FetchRequest, error) {
	req := search.FetchRequest{}

	url, ok := input["url"].(string)
	if !ok || url == "" {
		return req, fmt.Errorf("url is required and must be a string")
	}
	req.URL = url

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

	return fmt.Sprintf("%s\n\n%s\n\n%s", header, content, footer)
}
