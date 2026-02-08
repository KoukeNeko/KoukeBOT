package search

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
	md "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

const (
	fetchTimeout    = 30 * time.Second
	defaultMaxChars = 20000
	maxFetchSize    = 10 * 1024 * 1024 // 10MB 限制
)

// WebFetcher implements HTML → Markdown conversion for web_fetch tool
type WebFetcher struct {
	httpClient  *http.Client
	mdConverter *md.Converter
}

// NewWebFetcher creates a new web fetcher
func NewWebFetcher() *WebFetcher {
	converter := md.NewConverter()

	return &WebFetcher{
		httpClient: &http.Client{
			Timeout: fetchTimeout,
		},
		mdConverter: converter,
	}
}

// Fetch executes web page fetching and converts HTML to Markdown
func (f *WebFetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	// 驗證 URL
	if req.URL == "" {
		return FetchResult{}, fmt.Errorf("URL is required")
	}

	start := time.Now()

	// 1. HTTP GET with context
	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "KoukeBOT/1.0 (Web Fetch Tool)")
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.httpClient.Do(httpReq)
	if err != nil {
		return FetchResult{}, fmt.Errorf("http fetch: %w", err)
	}
	defer resp.Body.Close()

	// 檢查狀態碼
	if resp.StatusCode != http.StatusOK {
		return FetchResult{}, fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}

	// 2. 讀取 body (限制大小)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize))
	if err != nil {
		return FetchResult{}, fmt.Errorf("read body: %w", err)
	}

	// 3. 提取主要內容
	var htmlContent string
	var title string
	extractMode := req.ExtractMode
	if extractMode == "" {
		extractMode = "readability" // 預設使用 readability
	}

	if extractMode == "raw" {
		// Raw 模式：使用原始 HTML
		htmlContent = string(body)
		title = extractTitleFromHTML(htmlContent)
	} else {
		// Readability 模式：提取主要內容
		// 解析 URL string 為 *url.URL
		parsedURL, err := url.Parse(req.URL)
		if err != nil {
			return FetchResult{}, fmt.Errorf("parse URL: %w", err)
		}

		article, err := readability.FromReader(bytes.NewReader(body), parsedURL)
		if err != nil {
			// 降級到 raw 模式
			htmlContent = string(body)
			title = extractTitleFromHTML(htmlContent)
			extractMode = "raw" // 標記實際使用的模式
		} else {
			htmlContent = article.Content
			title = article.Title
		}
	}

	// 4. HTML → Markdown 轉換
	markdown, err := f.mdConverter.ConvertString(htmlContent)
	if err != nil {
		return FetchResult{}, fmt.Errorf("html to markdown: %w", err)
	}

	// 清理 Markdown（移除過多空行）
	markdown = cleanMarkdown(markdown)

	// 5. 截斷處理
	maxChars := req.MaxChars
	if maxChars == 0 {
		maxChars = defaultMaxChars
	}

	truncated := false
	if len(markdown) > maxChars {
		markdown = markdown[:maxChars] + "\n\n[...truncated]"
		truncated = true
	}

	result := FetchResult{
		URL:         req.URL,
		Title:       title,
		Content:     markdown,
		TextLength:  len(markdown),
		Truncated:   truncated,
		ExtractMode: extractMode,
	}

	logSearch.Logf("fetch: %s → %d chars, mode=%s (%dms)", req.URL, result.TextLength, extractMode, time.Since(start).Milliseconds())
	return result, nil
}

// extractTitleFromHTML 從 HTML 中提取 title
func extractTitleFromHTML(html string) string {
	// 簡單的 title 提取（降級方案）
	start := strings.Index(html, "<title>")
	if start == -1 {
		return "Untitled"
	}
	start += 7 // len("<title>")

	end := strings.Index(html[start:], "</title>")
	if end == -1 {
		return "Untitled"
	}

	return strings.TrimSpace(html[start : start+end])
}

// cleanMarkdown 清理 Markdown 格式
func cleanMarkdown(markdown string) string {
	// 移除連續超過 2 個的空行
	lines := strings.Split(markdown, "\n")
	var cleaned []string
	emptyCount := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			emptyCount++
			if emptyCount <= 2 {
				cleaned = append(cleaned, line)
			}
		} else {
			emptyCount = 0
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}
