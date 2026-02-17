package llm

import "strings"

// NormalizeModelID converts provider/model identifiers to a stable short model id.
// Examples:
// - " models/gemini-2.5-pro " -> "gemini-2.5-pro"
// - "projects/x/models/gemini-2.5-pro" -> "gemini-2.5-pro"
func NormalizeModelID(model string) string {
	normalized := strings.TrimSpace(model)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "models/") {
		normalized = strings.TrimPrefix(normalized, "models/")
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 && idx+1 < len(normalized) {
		normalized = normalized[idx+1:]
	}
	return strings.TrimSpace(normalized)
}

