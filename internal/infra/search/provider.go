package search

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logSearch = logger.New("search", logger.Green)

// SearchProvider 定義搜尋提供商的統一介面
type SearchProvider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) (SearchResult, error)
}

// RoutingMetadata OpenClaw 路由元資料
type RoutingMetadata struct {
	AutoRouted      bool    `json:"auto_routed"`
	Provider        string  `json:"provider"`
	Confidence      float64 `json:"confidence"`
	ConfidenceLevel string  `json:"confidence_level"` // "high", "medium", "low"
}

// FetchRequest web_fetch 工具請求
type FetchRequest struct {
	URL         string // 目標網址 (必填)
	MaxChars    int    // 截斷上限 (預設 20000)
	ExtractMode string // "readability" | "raw" (預設 "readability")
}

// FetchResult web_fetch 工具回應
type FetchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Content     string `json:"content"` // Markdown 格式
	TextLength  int    `json:"text_length"`
	Truncated   bool   `json:"truncated"`
	ExtractMode string `json:"extract_mode"`
}

// ProviderRouter 根據設定選擇搜尋提供商
type ProviderRouter struct {
	braveClient       *BraveSearchClient
	perplexityClient  *PerplexityClient
	preferredProvider string // "brave" | "perplexity" | "auto"
}

// NewProviderRouter 建立提供商路由器
func NewProviderRouter(braveKey, perplexityKey, preferred string) *ProviderRouter {
	router := &ProviderRouter{
		preferredProvider: preferred,
	}

	if braveKey != "" {
		router.braveClient = NewBraveSearchClient(braveKey)
	}
	if perplexityKey != "" {
		router.perplexityClient = NewPerplexityClient(perplexityKey)
	}

	// Auto 模式預設 Brave (更穩定)
	if preferred == "auto" || preferred == "" {
		if router.braveClient != nil {
			router.preferredProvider = "brave"
		} else if router.perplexityClient != nil {
			router.preferredProvider = "perplexity"
		}
	}

	return router
}

// Name 返回當前提供商名稱
func (r *ProviderRouter) Name() string {
	return fmt.Sprintf("router(%s)", r.preferredProvider)
}

// Search 路由到對應提供商
func (r *ProviderRouter) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	switch r.preferredProvider {
	case "perplexity":
		if r.perplexityClient != nil {
			return r.perplexityClient.Search(ctx, req)
		}
		// Fallback to Brave
		if r.braveClient != nil {
			logSearch.Warnf("Perplexity not available, falling back to Brave")
			return r.braveClient.Search(ctx, req)
		}

	case "brave":
		if r.braveClient != nil {
			return r.braveClient.Search(ctx, req)
		}
		// Fallback to Perplexity
		if r.perplexityClient != nil {
			logSearch.Warnf("Brave not available, falling back to Perplexity")
			return r.perplexityClient.Search(ctx, req)
		}
	}

	return SearchResult{}, fmt.Errorf("no search provider configured")
}

// IsConfigured 檢查是否至少有一個提供商已設定
func (r *ProviderRouter) IsConfigured() bool {
	return r.braveClient != nil || r.perplexityClient != nil
}
