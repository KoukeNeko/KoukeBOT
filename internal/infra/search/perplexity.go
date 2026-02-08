package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	perplexityEndpoint = "https://api.perplexity.ai/chat/completions"
	perplexityModel    = "sonar" // 或使用 "sonar-pro"
	perplexityTimeout  = 30 * time.Second
)

// PerplexityClient implements web search via Perplexity Sonar API
type PerplexityClient struct {
	apiKey     string
	httpClient *http.Client
	cache      *searchCache
	model      string
}

// perplexityRequest OpenAI 相容格式
type perplexityRequest struct {
	Model    string              `json:"model"`
	Messages []perplexityMessage `json:"messages"`
}

// perplexityMessage 訊息結構
type perplexityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// perplexityResponse Perplexity API 回應
type perplexityResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Citations []string `json:"citations,omitempty"` // Sonar 提供引用
	Usage     struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewPerplexityClient creates a new Perplexity Sonar API client
func NewPerplexityClient(apiKey string) *PerplexityClient {
	return &PerplexityClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: perplexityTimeout,
		},
		cache: newSearchCache(),
		model: perplexityModel,
	}
}

// Name returns the provider name (實作 SearchProvider 介面)
func (c *PerplexityClient) Name() string {
	return "perplexity"
}

// Search performs a web search using Perplexity Sonar
func (c *PerplexityClient) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	// 檢查快取
	cacheKey := c.buildCacheKey(req)
	if cached, found := c.cache.get(cacheKey); found {
		cached.Cached = true
		logSearch.Logf("perplexity: q=%q → %d results (cached)", req.Query, cached.Count)
		return cached, nil
	}

	// 構建請求
	payload := perplexityRequest{
		Model: c.model,
		Messages: []perplexityMessage{
			{
				Role:    "user",
				Content: req.Query,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return SearchResult{}, fmt.Errorf("marshal request: %w", err)
	}

	// 發送 HTTP 請求
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", perplexityEndpoint, bytes.NewReader(body))
	if err != nil {
		return SearchResult{}, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return SearchResult{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// 檢查狀態碼
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return SearchResult{}, fmt.Errorf("perplexity API error: %d - %s", resp.StatusCode, string(body))
	}

	// 解析回應
	var perplexityResp perplexityResponse
	if err := json.NewDecoder(resp.Body).Decode(&perplexityResp); err != nil {
		return SearchResult{}, fmt.Errorf("parse response: %w", err)
	}

	// 轉換為標準格式
	result := SearchResult{
		Query:    req.Query,
		Provider: "perplexity",
		TookMs:   time.Since(start).Milliseconds(),
		Results:  make([]SearchResultItem, 0),
		Routing: &RoutingMetadata{
			AutoRouted:      false,
			Provider:        "perplexity",
			Confidence:      0.95,
			ConfidenceLevel: "high",
		},
	}

	// Perplexity 回傳 AI 合成答案 + 引用
	if len(perplexityResp.Choices) > 0 {
		content := perplexityResp.Choices[0].Message.Content

		// 將答案包裝為主要結果
		result.Results = append(result.Results, SearchResultItem{
			Title:       "Perplexity AI 摘要",
			URL:         "",
			Description: truncateString(content, 500),
			Score:       1.0,
		})

		// 加入引用作為額外結果
		for i, citation := range perplexityResp.Citations {
			result.Results = append(result.Results, SearchResultItem{
				Title:       fmt.Sprintf("來源 %d", i+1),
				URL:         citation,
				Description: "引用來源",
				Score:       0.8,
			})
		}
	}

	result.Count = len(result.Results)

	// 快取結果
	c.cache.set(cacheKey, result)

	logSearch.Logf("perplexity: q=%q → %d results, %d citations (%dms)", req.Query, result.Count, len(perplexityResp.Citations), result.TookMs)
	return result, nil
}

// buildCacheKey creates a unique cache key from request parameters
func (c *PerplexityClient) buildCacheKey(req SearchRequest) string {
	return fmt.Sprintf("perplexity|%s|%d", req.Query, req.Count)
}
