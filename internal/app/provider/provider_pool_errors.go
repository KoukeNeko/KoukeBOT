package provider

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	errorQuota                // 429 / RESOURCE_EXHAUSTED / quota exceeded
	errorBilling              // 402 / payment required / insufficient credits
	errorPermissionDenied     // 403 / IAM permission denied — account lacks access, disable and try next
	errorOverloaded           // Server overloaded (retryable, short cooldown)
	errorModelCapacity        // Model server has no capacity (distinct from account quota)
	errorModelUnavailable     // Model not found / not supported
	errorValidation           // Invalid request format
	errorTransient            // 5xx / timeout / connection issues
	errorEmptyStream          // 200 OK but stream returned no content
	errorOther
)

// classifyError determines the error type for failover logic.
//
// Check order matters: model capacity must precede generic quota (both
// contain "429"/"RESOURCE_EXHAUSTED"), billing must precede quota (402 ≠ 429),
// and overloaded must precede transient (needs shorter cooldown).
func classifyError(err error) errorType {
	if err == nil {
		return errorNone
	}
	if errors.Is(err, ErrEmptyStream) || strings.Contains(err.Error(), "empty stream") {
		return errorEmptyStream
	}
	s := err.Error()
	lower := strings.ToLower(s)

	if isModelCapacityError(lower) {
		return errorModelCapacity
	}
	if isModelUnavailableError(lower) {
		return errorModelUnavailable
	}
	if isBillingError(lower) {
		return errorBilling
	}
	if isPermissionDeniedError(lower) {
		return errorPermissionDenied
	}
	if isOverloadedError(lower) {
		return errorOverloaded
	}
	if isQuotaLikeError(s, lower) {
		return errorQuota
	}
	if isValidationError(lower) {
		return errorValidation
	}
	if isTransientError(s, lower) {
		return errorTransient
	}

	return errorOther
}

// ErrEmptyStream indicates a 200 OK response with no content in the stream.
var ErrEmptyStream = errors.New("empty stream: server returned 200 OK but no content")

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

// isModelUnavailableError detects model-not-found or unsupported-modality errors.
// Covers both HTTP 404 responses and provider-specific "not supported" messages
// for multimodal inputs when the selected model does not support vision.
func isModelUnavailableError(lower string) bool {
	if strings.Contains(lower, "http 404") ||
		strings.Contains(lower, "\"status\": \"not_found\"") ||
		strings.Contains(lower, "\"status\":\"not_found\"") ||
		strings.Contains(lower, "requested entity was not found") {
		return true
	}

	return strings.Contains(lower, "multimodal") && strings.Contains(lower, "not support") ||
		strings.Contains(lower, "image") && strings.Contains(lower, "not support") ||
		strings.Contains(lower, "unsupported image input")
}

// modelCapacityCooldown returns the global cooldown applied when the model
// server itself has no capacity (longer than per-account quota cooldowns).
func modelCapacityCooldown() time.Duration {
	return 90 * time.Second
}

// isBillingError detects payment/credit-related failures (typically HTTP 402).
func isBillingError(lower string) bool {
	return strings.Contains(lower, "http 402") ||
		strings.Contains(lower, "payment required") ||
		strings.Contains(lower, "insufficient credits") ||
		strings.Contains(lower, "insufficient balance") ||
		strings.Contains(lower, "credit balance")
}

// isPermissionDeniedError detects IAM/OAuth permission failures (HTTP 403).
// These indicate the account lacks the necessary IAM role for this endpoint
// (e.g. cloudaicompanion.companions.generateChat), so the account should be
// disabled and the next available account tried instead.
func isPermissionDeniedError(lower string) bool {
	return strings.Contains(lower, "iam_permission_denied") ||
		strings.Contains(lower, "permission_denied") ||
		(strings.Contains(lower, "http 403") && strings.Contains(lower, "permission"))
}

// isOverloadedError detects server overload conditions (retryable with short cooldown).
func isOverloadedError(lower string) bool {
	return strings.Contains(lower, "overloaded_error") ||
		strings.Contains(lower, "overloaded")
}

// isQuotaLikeError detects quota/rate-limit errors from both status codes and body text.
// Takes both original (s) and lowercased (lower) forms because "RESOURCE_EXHAUSTED" is case-sensitive.
func isQuotaLikeError(s, lower string) bool {
	return strings.Contains(s, "429") ||
		strings.Contains(s, "RESOURCE_EXHAUSTED") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "usage limit") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "exceeded your current quota")
}

// isValidationError detects malformed request errors (invalid tool call IDs, schema violations, etc.).
// These are non-retryable and indicate a bug in the request construction.
func isValidationError(lower string) bool {
	return strings.Contains(lower, "tool_use_id") ||
		strings.Contains(lower, "invalid_request_error") ||
		strings.Contains(lower, "validation")
}

// isTransientError detects temporary infrastructure failures (5xx, timeouts, network resets).
// These are retryable and should trigger a short cooldown before the next attempt.
// Takes both original (s) and lowercased (lower) forms because "EOF" is uppercase.
func isTransientError(s, lower string) bool {
	return strings.Contains(lower, "http 5") ||
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
		strings.Contains(s, "EOF")
}

// isQuotaError checks if an error indicates a 429/RESOURCE_EXHAUSTED response.
func isQuotaError(err error) bool {
	return classifyError(err) == errorQuota
}

// Regex patterns for extracting reset delay from error messages.
// Priority order: HTTP headers (injected by openStreamGenerateContent) → body JSON → human-readable text.
var (
	retryAfterHeaderRe    = regexp.MustCompile(`\[retry-after:(\d+)]`)
	rateLimitResetAfterRe = regexp.MustCompile(`\[x-ratelimit-reset-after:(\d+)]`)
	retryDelayJSONRe      = regexp.MustCompile(`"retryDelay"\s*:\s*"(\d+)s"`)
	quotaResetDelayRe     = regexp.MustCompile(`"quotaResetDelay"\s*:\s*"(\d+)s"`)
	humanResetDelayRe     = regexp.MustCompile(`reset (?:after|in) ((?:\d+h)?(?:\d+m)?(?:\d+s)?)`)
)

// parseResetDelay extracts the quota reset duration from an error message.
// Checks HTTP response headers first, then body JSON patterns, then human-readable text.
func parseResetDelay(errMsg string) time.Duration {
	// 1. HTTP header: retry-after (injected by openStreamGenerateContent)
	if d := parseSecondsFromRegex(retryAfterHeaderRe, errMsg); d > 0 {
		return d
	}
	// 2. HTTP header: x-ratelimit-reset-after
	if d := parseSecondsFromRegex(rateLimitResetAfterRe, errMsg); d > 0 {
		return d
	}
	// 3. Body JSON: "quotaResetDelay":"Xs"
	if d := parseSecondsFromRegex(quotaResetDelayRe, errMsg); d > 0 {
		return d
	}
	// 4. Body JSON: "retryDelay":"Xs" (Gemini format)
	if d := parseSecondsFromRegex(retryDelayJSONRe, errMsg); d > 0 {
		return d
	}
	// 5. Human-readable: "reset after 1m30s" / "reset in 45s"
	matches := humanResetDelayRe.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		if d, err := time.ParseDuration(matches[1]); err == nil && d > 0 {
			return d
		}
	}

	return defaultResetDelay
}

const defaultResetDelay = 2 * time.Minute

// parseSecondsFromRegex extracts a duration in seconds from the first capture group.
func parseSecondsFromRegex(re *regexp.Regexp, s string) time.Duration {
	matches := re.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0
	}
	seconds, err := strconv.Atoi(matches[1])
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// quotaCooldownDuration returns an escalating cooldown for rate-limit errors.
// Formula: 60s * 5^(errorCount-1), capped at 1 hour.
// Sequence: 1m → 5m → 25m → 1h → 1h ...
func quotaCooldownDuration(errorCount int) time.Duration {
	if errorCount < 1 {
		errorCount = 1
	}
	base := 60 * time.Second
	multiplier := math.Pow(5, float64(errorCount-1))
	d := time.Duration(float64(base) * multiplier)
	if d > time.Hour {
		d = time.Hour
	}
	return addJitter(d, 5*time.Second)
}

// billingDisableDuration returns a long disable duration for billing/payment errors.
// Formula: 5h * 2^(errorCount-1), capped at 24 hours.
// Sequence: 5h → 10h → 20h → 24h ...
func billingDisableDuration(errorCount int) time.Duration {
	if errorCount < 1 {
		errorCount = 1
	}
	base := 5 * time.Hour
	d := base * time.Duration(1<<(errorCount-1))
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return d
}

// overloadedCooldown returns a short cooldown for server overload errors.
func overloadedCooldown() time.Duration {
	return addJitter(10*time.Second, 5*time.Second)
}

// transientCooldown returns an escalating cooldown for transient infrastructure errors.
// Formula: base * 2^(failureCount-1), capped at 30 seconds, with ±250ms jitter.
// Sequence (base=2s): 2s → 4s → 8s → 16s → 30s → 30s ...
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

// exponentialBackoff returns a delay for retry loops (e.g., empty-stream retries).
// Formula: base * 2^retry, capped at 30 seconds, with ±500ms jitter.
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
