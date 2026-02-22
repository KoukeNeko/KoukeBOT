package chat

import (
	"regexp"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

var imageURLRegex = regexp.MustCompile(`(?i)https?://[^\s<>()"'` + "`" + `]+?\.(?:png|jpe?g|gif|webp)(?:\?[^\s<>()"'` + "`" + `]*)?`)

func collectGeneratedImageURLs(results []domain.ToolResult) []string {
	urls := make([]string, 0)
	for _, result := range results {
		if result.IsError {
			continue
		}
		extracted := extractGeneratedImageURLs(result.Content)
		urls = appendUniqueStrings(urls, extracted)
	}
	return urls
}

func extractGeneratedImageURLs(text string) []string {
	matches := imageURLRegex.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		cleaned := strings.TrimSpace(match)
		if cleaned == "" {
			continue
		}
		out = appendUniqueStrings(out, []string{cleaned})
	}
	return out
}

func appendGeneratedImageURLs(raw string, urls []string) string {
	normalized := appendUniqueStrings(nil, urls)
	if len(normalized) == 0 {
		return raw
	}

	missing := make([]string, 0, len(normalized))
	for _, url := range normalized {
		if !strings.Contains(raw, url) {
			missing = append(missing, url)
		}
	}
	if len(missing) == 0 {
		return raw
	}

	var b strings.Builder
	base := strings.TrimSpace(raw)
	if base != "" {
		b.WriteString(base)
		b.WriteString("\n\n")
	}
	if len(missing) == 1 {
		b.WriteString("Generated image:\n")
	} else {
		b.WriteString("Generated images:\n")
	}
	for i, url := range missing {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(url)
	}
	return b.String()
}

func appendUniqueStrings(base []string, candidates []string) []string {
	out := make([]string, 0, len(base)+len(candidates))
	seen := make(map[string]struct{}, len(base)+len(candidates))
	for _, item := range base {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, item := range candidates {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
