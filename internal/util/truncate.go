package util

// TruncateWithEllipsis cuts s to maxLen characters, appending "..." if truncated.
// Returns the original string unchanged if it fits within maxLen.
func TruncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
