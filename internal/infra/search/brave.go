package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	braveAPIEndpoint = "https://api.search.brave.com/res/v1/web/search"
	defaultTimeout   = 30 * time.Second
	defaultCount     = 5
)

// BraveSearchClient implements web search via Brave Search API
type BraveSearchClient struct {
	apiKey     string
	httpClient *http.Client
	cache      *searchCache
}

// SearchRequest represents a search query with parameters
type SearchRequest struct {
	Query      string // Search query (required)
	Count      int    // Number of results (1-10, default 5)
	Country    string // Country code (e.g., "US", "TW", "ALL")
	SearchLang string // Search language (e.g., "en", "zh-TW")
	Freshness  string // Time filter: "pd", "pw", "pm", "py", or date range
}

// SearchResult represents the search response (OpenClaw 相容增強版)
type SearchResult struct {
	Query    string             `json:"query"`
	Provider string             `json:"provider"`
	Count    int                `json:"count"`
	TookMs   int64              `json:"took_ms"`
	Results  []SearchResultItem `json:"results"`
	Cached   bool               `json:"cached,omitempty"`
	Routing  *RoutingMetadata   `json:"routing,omitempty"` // OpenClaw 相容：路由元資料
}

// SearchResultItem represents a single search result (OpenClaw 相容增強版)
type SearchResultItem struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	Published   string  `json:"published,omitempty"`
	SiteName    string  `json:"site_name,omitempty"`
	Score       float64 `json:"score,omitempty"` // OpenClaw 相容：相關度評分
}

// braveAPIResponse represents Brave's API response format
type braveAPIResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Age         string `json:"age"`
			PageAge     string `json:"page_age"`
			Meta        struct {
				SiteName string `json:"site_name"`
			} `json:"meta_url"`
		} `json:"results"`
	} `json:"web"`
}

// NewBraveSearchClient creates a new Brave Search API client
func NewBraveSearchClient(apiKey string) *BraveSearchClient {
	return &BraveSearchClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		cache: newSearchCache(),
	}
}

// Name returns the provider name (實作 SearchProvider 介面)
func (c *BraveSearchClient) Name() string {
	return "brave"
}

// Search performs a web search and returns results
func (c *BraveSearchClient) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	// Normalize query for cache key
	cacheKey := c.buildCacheKey(req)

	// Check cache first
	if cached, found := c.cache.get(cacheKey); found {
		cached.Cached = true
		return cached, nil
	}

	// Build API request
	apiURL, err := c.buildURL(req)
	if err != nil {
		return SearchResult{}, fmt.Errorf("build URL: %w", err)
	}

	// Execute request
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return SearchResult{}, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Encoding", "gzip")
	httpReq.Header.Set("X-Subscription-Token", c.apiKey)
	httpReq.Header.Set("Cache-Control", "no-cache") // 修正 Brave API 422 錯誤

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return SearchResult{}, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return SearchResult{}, fmt.Errorf("Brave API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var braveResp braveAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return SearchResult{}, fmt.Errorf("parse response: %w", err)
	}

	// Convert to our format
	result := SearchResult{
		Query:    req.Query,
		Provider: "brave",
		Count:    len(braveResp.Web.Results),
		TookMs:   time.Since(start).Milliseconds(),
		Results:  make([]SearchResultItem, 0, len(braveResp.Web.Results)),
		Cached:   false,
		Routing: &RoutingMetadata{
			AutoRouted:      false,
			Provider:        "brave",
			Confidence:      1.0,
			ConfidenceLevel: "high",
		},
	}

	for i, item := range braveResp.Web.Results {
		// OpenClaw 相容：根據排名計算相關度評分 (1.0 → 0.1)
		score := 1.0 - (float64(i) * 0.1)
		if score < 0.1 {
			score = 0.1
		}

		result.Results = append(result.Results, SearchResultItem{
			Title:       item.Title,
			URL:         item.URL,
			Description: truncateString(item.Description, 200),
			Published:   item.Age,
			SiteName:    item.Meta.SiteName,
			Score:       score,
		})
	}

	// Store in cache
	c.cache.set(cacheKey, result)

	return result, nil
}

// buildURL constructs the Brave API URL with query parameters
func (c *BraveSearchClient) buildURL(req SearchRequest) (string, error) {
	if req.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	count := req.Count
	if count == 0 {
		count = defaultCount
	}
	if count < 1 || count > 10 {
		return "", fmt.Errorf("count must be between 1 and 10")
	}

	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("count", strconv.Itoa(count))

	if req.Country != "" {
		params.Set("country", req.Country)
	}
	if req.SearchLang != "" {
		params.Set("search_lang", req.SearchLang)
	}
	if req.Freshness != "" {
		params.Set("freshness", req.Freshness)
	}

	return braveAPIEndpoint + "?" + params.Encode(), nil
}

// buildCacheKey creates a unique cache key from request parameters
func (c *BraveSearchClient) buildCacheKey(req SearchRequest) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s",
		req.Query, req.Count, req.Country, req.SearchLang, req.Freshness)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
