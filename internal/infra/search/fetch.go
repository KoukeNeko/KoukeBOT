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
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
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
	converter := md.NewConverter(
		md.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
		),
	)

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
		// Raw 模式：使用原始 HTML，但嘗試提取主要內容區域
		htmlContent = extractMainContent(string(body))
		title = extractTitleFromHTML(string(body))
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

			// 智能檢測：如果提取的內容看起來是錯誤的（例如服務條款），則回退到 raw 模式
			if isLikelyIncorrectExtraction(htmlContent) {
				logSearch.Warnf("readability extracted incorrect content (likely terms/privacy), falling back to raw mode")
				htmlContent = extractMainContent(string(body))
				title = extractTitleFromHTML(string(body))
				extractMode = "raw-fallback" // 標記為自動回退
			}
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

// extractMainContent 從完整 HTML 中嘗試提取主要內容區域
func extractMainContent(html string) string {
	// 嘗試按優先順序尋找主要內容標記
	// 注意：優先使用更具體的 class/id 選擇器，而非通用的 <main>/<article> 標籤
	patterns := []struct {
		start string
		end   string
	}{
		// 1. 具體的內容 class（最精確）
		{`<div class="editor article"`, `</div>`},
		{`<article class="content"`, `</article>`},
		{`<div class="article-content"`, `</div>`},
		{`<div class="post-content"`, `</div>`},
		// 2. <article> 標籤（語義明確）
		{"<article", "</article>"},
		// 3. 通用的 content 標記
		{`<div class="content"`, `</div>`},
		{`<div id="content"`, `</div>`},
		{`<div id="main-content"`, `</div>`},
		// 4. <main> 標籤（可能範圍太大，最後才用）
		{"<main", "</main>"},
	}

	for _, pattern := range patterns {
		startIdx := strings.Index(html, pattern.start)
		if startIdx == -1 {
			continue
		}

		// 找到開始標籤的結束位置
		tagEnd := strings.Index(html[startIdx:], ">")
		if tagEnd == -1 {
			continue
		}
		contentStart := startIdx + tagEnd + 1

		// 提取標籤名稱（用於匹配嵌套）
		var tagName string
		if strings.HasPrefix(pattern.start, "<") {
			// 提取標籤名（例如從 "<div" 提取 "div"）
			tagNameEnd := strings.IndexAny(pattern.start[1:], " >")
			if tagNameEnd == -1 {
				tagName = pattern.start[1:]
			} else {
				tagName = pattern.start[1 : tagNameEnd+1]
			}
		}

		// 找到對應的結束標籤（處理嵌套）
		var content string
		if tagName != "" {
			content = findMatchingClosingTag(html[contentStart:], tagName)
		} else {
			// 非標籤模式，使用簡單匹配
			endIdx := strings.Index(html[contentStart:], pattern.end)
			if endIdx == -1 {
				continue
			}
			content = html[contentStart : contentStart+endIdx]
		}

		// 如果找到的內容長度合理（大於 100 字符），就使用它
		if content != "" && len(strings.TrimSpace(content)) > 100 {
			return content
		}
	}

	// 如果都沒找到，返回整個 HTML
	return html
}

// findMatchingClosingTag 找到匹配的結束標籤，處理嵌套情況
func findMatchingClosingTag(html, tagName string) string {
	openTag := "<" + tagName
	closeTag := "</" + tagName + ">"

	depth := 1 // 已經在一個標籤內部
	pos := 0

	for pos < len(html) && depth > 0 {
		// 尋找下一個可能的標籤
		nextOpen := strings.Index(html[pos:], openTag)
		nextClose := strings.Index(html[pos:], closeTag)

		if nextClose == -1 {
			// 沒有找到結束標籤
			return ""
		}

		if nextOpen != -1 && nextOpen < nextClose {
			// 找到了嵌套的開始標籤
			// 確認這確實是一個標籤開始（後面是空格、>、或屬性）
			checkPos := pos + nextOpen + len(openTag)
			if checkPos < len(html) {
				nextChar := html[checkPos]
				if nextChar == ' ' || nextChar == '>' || nextChar == '\t' || nextChar == '\n' {
					depth++
					pos += nextOpen + len(openTag)
					continue
				}
			}
			pos += nextOpen + 1
		} else {
			// 找到了結束標籤
			if depth == 1 {
				// 這是匹配的結束標籤
				return html[:pos+nextClose]
			}
			depth--
			pos += nextClose + len(closeTag)
		}
	}

	return ""
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

// isLikelyIncorrectExtraction 檢測 readability 提取的內容是否可能是錯誤的
// 例如：服務條款、隱私政策等
func isLikelyIncorrectExtraction(content string) bool {
	// 將內容轉為小寫以便不區分大小寫比對
	lowerContent := strings.ToLower(content)

	// 常見的錯誤提取內容關鍵詞（中英文）
	incorrectKeywords := []string{
		"服務條款", "使用條款", "terms of service", "terms and conditions",
		"隱私權政策", "隱私政策", "privacy policy",
		"個人資料保護", "個人隱私權", "personal data protection",
		"cookie policy", "cookie 政策",
		"免責聲明", "disclaimer",
		"法律聲明", "legal notice",
	}

	// 計算匹配的關鍵詞數量
	matchCount := 0
	for _, keyword := range incorrectKeywords {
		if strings.Contains(lowerContent, strings.ToLower(keyword)) {
			matchCount++
		}
	}

	// 如果內容中出現 3 個或以上的相關關鍵詞，很可能是錯誤提取
	if matchCount >= 3 {
		return true
	}

	// 額外檢查：如果內容開頭就是這些關鍵詞，也可能是錯誤的
	contentStart := lowerContent
	if len(contentStart) > 500 {
		contentStart = contentStart[:500]
	}

	criticalKeywords := []string{
		"本網站服務使用條款",
		"網站服務使用條款",
		"terms of service",
		"privacy policy",
	}

	for _, keyword := range criticalKeywords {
		if strings.Contains(contentStart, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
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
