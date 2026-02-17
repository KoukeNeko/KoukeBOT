package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// errorType classifies errors for appropriate failover handling.
type errorType int

const (
	errorNone errorType = iota
	errorQuota
	errorModelCapacity // Model server has no capacity (distinct from account quota)
	errorValidation
	errorTransient
	errorOther
)

// classifyError determines the error type for failover logic.
func classifyError(err error) errorType {
	if err == nil {
		return errorNone
	}
	s := err.Error()
	lower := strings.ToLower(s)

	// Model capacity must be checked before generic quota because both
	// contain "429" / "RESOURCE_EXHAUSTED", but model capacity requires
	// different handling (global cooldown instead of account rotation).
	if isModelCapacityError(lower) {
		return errorModelCapacity
	}

	if strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") {
		return errorQuota
	}

	if strings.Contains(lower, "tool_use_id") ||
		strings.Contains(lower, "invalid_request_error") ||
		strings.Contains(lower, "validation") {
		return errorValidation
	}

	if strings.Contains(lower, "http 5") ||
		strings.Contains(lower, "internal server error") ||
		strings.Contains(lower, "bad gateway") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "gateway timeout") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "temporary") ||
		strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(s, "EOF") {
		return errorTransient
	}

	return errorOther
}

func isContextTerminated(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "context canceled")
}

// isModelCapacityError checks for model-level capacity exhaustion keywords.
func isModelCapacityError(lower string) bool {
	return strings.Contains(lower, "no capacity available") ||
		strings.Contains(lower, "model_capacity") ||
		strings.Contains(lower, "model capacity")
}

// modelCapacityCooldown returns the global cooldown applied when the model
// server itself has no capacity (longer than per-account quota cooldowns).
func modelCapacityCooldown() time.Duration {
	return 90 * time.Second
}

// isQuotaError checks if an error indicates a 429/RESOURCE_EXHAUSTED response.
func isQuotaError(err error) bool {
	return classifyError(err) == errorQuota
}

var quotaResetDelayRe = regexp.MustCompile(`"quotaResetDelay"\s*:\s*"(\d+)s"`)
var humanResetDelayRe = regexp.MustCompile(`reset (?:after|in) ((?:\d+h)?(?:\d+m)?(?:\d+s)?)`)

// parseResetDelay extracts the quota reset duration from an error message.
func parseResetDelay(errMsg string) time.Duration {
	matches := quotaResetDelayRe.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		seconds, err := strconv.Atoi(matches[1])
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	matches = humanResetDelayRe.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		if d, err := time.ParseDuration(matches[1]); err == nil && d > 0 {
			return d
		}
	}

	// "No capacity available" is now classified as errorModelCapacity and
	// handled via modelCapacityCooldown(); no special case needed here.
	return 2 * time.Minute
}

func transientCooldown(base time.Duration, failureCount int) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if failureCount < 1 {
		failureCount = 1
	}
	cooldown := base * time.Duration(1<<(failureCount-1))
	if cooldown > 30*time.Second {
		cooldown = 30 * time.Second
	}
	return addJitter(cooldown, 250*time.Millisecond)
}

func exponentialBackoff(base time.Duration, retry int) time.Duration {
	if base <= 0 {
		base = 1 * time.Second
	}
	if retry < 0 {
		retry = 0
	}
	delay := base * time.Duration(1<<retry)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return addJitter(delay, 500*time.Millisecond)
}

func addJitter(base, maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(maxJitter)))
}

func truncateError(err error, maxLen int) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatDuration returns a compact human-readable duration (e.g., "6h22m").
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
