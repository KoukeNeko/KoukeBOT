package app

import (
	"testing"
	"time"
)

func TestParseResetDelay(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected time.Duration
	}{
		{
			name:     "JSON format with quotaResetDelay",
			errMsg:   `{"error":{"code":429,"message":"quota exceeded","details":[{"quotaResetDelay":"123s"}]}}`,
			expected: 123 * time.Second,
		},
		{
			name:     "Human-readable: reset after 1h38m34s",
			errMsg:   `You have exhausted your capacity on this model. Your quota will reset after 1h38m34s.`,
			expected: 1*time.Hour + 38*time.Minute + 34*time.Second,
		},
		{
			name:     "Human-readable: reset in 5m",
			errMsg:   `Rate limit exceeded. Your quota will reset in 5m.`,
			expected: 5 * time.Minute,
		},
		{
			name:     "Human-readable: reset after 2h",
			errMsg:   `Quota exceeded. Your quota will reset after 2h.`,
			expected: 2 * time.Hour,
		},
		{
			name:     "No capacity available (transient overload)",
			errMsg:   `No capacity available for model gemini-3-pro-preview on the server`,
			expected: 30 * time.Second,
		},
		{
			name:     "Unparseable error (fallback)",
			errMsg:   `Some unknown error format`,
			expected: 2 * time.Minute,
		},
		{
			name:     "Empty error message (fallback)",
			errMsg:   ``,
			expected: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseResetDelay(tt.errMsg)
			if result != tt.expected {
				t.Errorf("parseResetDelay(%q) = %v, expected %v", tt.errMsg, result, tt.expected)
			}
		})
	}
}
