package llm

// gemini_oauth_http.go — HTTP transport layer for GeminiOAuthProvider.
//
// Responsibility: all code that touches net/http — request dispatch, exponential
// backoff retry, rate-limit header extraction, and endpoint rotation. Isolating
// transport here lets the retry strategy be changed without touching provider or
// SSE parsing logic.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// resolveOAuthEndpointCandidates filters blank entries from the candidate list
// and falls back to the default primary/sandbox pair when the list is empty.
func resolveOAuthEndpointCandidates(candidates []string, endpointPolicy string) []string {
	policy := ResolveEndpointPolicy(endpointPolicy)
	resolved := make([]string, 0, len(candidates))
	for _, endpoint := range candidates {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		resolved = append(resolved, endpoint)
	}
	if len(resolved) == 0 {
		resolved = []string{codeAssistPrimaryEndpoint, codeAssistSandboxEndpoint}
	}
	if policy != EndpointPolicyPrimaryThenSandbox {
		return resolved
	}
	return resolved
}

func setCodeAssistHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)
}

// HTTP retry constants (aligned with pi-ai's google-gemini-cli provider).
const (
	httpMaxRetries     = 3              // Total retry attempts (4 attempts including initial)
	httpBaseDelay      = 1 * time.Second
	httpMaxServerDelay = 60 * time.Second // If server requests longer, return error to pool layer
)

// openStreamGenerateContent sends a POST to the streamGenerateContent endpoint,
// rotating across endpoint candidates on each retry and applying exponential backoff.
func (g *GeminiOAuthProvider) openStreamGenerateContent(ctx context.Context, body []byte, label string) (*http.Response, string, error) {
	candidates := g.currentEndpointCandidates()
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("%s: no endpoint candidates available", label)
	}

	var lastErr error
	var lastErrEndpoint string

	for attempt := 0; attempt <= httpMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, lastErrEndpoint, ctx.Err()
		}

		// Rotate endpoint on each attempt (pi-ai: endpoints[min(attempt, len-1)]).
		endpointIdx := attempt
		if endpointIdx >= len(candidates) {
			endpointIdx = len(candidates) - 1
		}
		endpoint := candidates[endpointIdx]

		reqURL := strings.TrimRight(endpoint, "/") + "/v1internal:streamGenerateContent?alt=sse"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
		if err != nil {
			return nil, endpoint, fmt.Errorf("create request: %w", err)
		}
		setCodeAssistHeaders(httpReq)

		resp, err := g.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", label, err)
			lastErrEndpoint = endpoint
			if attempt < httpMaxRetries {
				delay := httpBackoffDelay(attempt)
				logOAuth.Warnf("%s endpoint=%s attempt=%d failed (%v), retrying in %s",
					label, endpoint, attempt+1, err, delay)
				if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
					return nil, endpoint, sleepErr
				}
				continue
			}
			return nil, endpoint, lastErr
		}

		if resp.StatusCode == http.StatusOK {
			g.setEffectiveEndpoint(endpoint)
			return resp, endpoint, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Embed rate-limit headers into the error message so the upper-layer
		// parseResetDelay() can extract the server's recommended wait time.
		headerInfo := extractRateLimitHeaders(resp)
		formattedErr := fmt.Errorf("%s HTTP %d:%s %s", label, resp.StatusCode, headerInfo, string(respBody))
		lastErr = formattedErr
		lastErrEndpoint = endpoint

		// Check if this error is retryable at the HTTP level.
		if attempt < httpMaxRetries && isHTTPRetryable(resp.StatusCode, string(respBody)) {
			// Extract server-recommended delay from headers and body.
			serverDelay := extractHTTPRetryDelay(string(respBody), resp)

			// If server requests more than httpMaxServerDelay, stop retrying at HTTP
			// level and let the pool layer handle it with account rotation.
			if serverDelay > httpMaxServerDelay {
				logOAuth.Warnf("%s endpoint=%s status=%d: server requested %s delay (max %s), deferring to pool",
					label, endpoint, resp.StatusCode, serverDelay, httpMaxServerDelay)
				return nil, endpoint, formattedErr
			}

			delay := httpBackoffDelay(attempt)
			if serverDelay > delay {
				delay = serverDelay
			}
			logOAuth.Warnf("%s endpoint=%s status=%d attempt=%d, retrying in %s",
				label, endpoint, resp.StatusCode, attempt+1, delay)
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, endpoint, sleepErr
			}
			continue
		}

		return nil, endpoint, formattedErr
	}

	if lastErr != nil {
		return nil, lastErrEndpoint, lastErr
	}
	return nil, "", fmt.Errorf("%s: no endpoint candidates available", label)
}

// isHTTPRetryable checks if an HTTP error should be retried at the transport level.
// Aligned with pi-ai's isRetryableError: 429, 5xx, and retryable body patterns.
func isHTTPRetryable(statusCode int, body string) bool {
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return true
	}
	return httpRetryableBodyRe.MatchString(body)
}

var httpRetryableBodyRe = regexp.MustCompile(`(?i)resource.?exhausted|rate.?limit|overloaded|service.?unavailable|other.?side.?closed`)

// httpBackoffDelay returns exponential backoff: 1s, 2s, 4s, …
func httpBackoffDelay(attempt int) time.Duration {
	return httpBaseDelay * time.Duration(math.Pow(2, float64(attempt)))
}

// sleepWithContext sleeps for the given duration, returning early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// extractHTTPRetryDelay extracts the server-recommended retry delay from
// HTTP response headers and body text. Returns 0 if no delay is found.
// Aligned with pi-ai's extractRetryDelay.
func extractHTTPRetryDelay(errorText string, resp *http.Response) time.Duration {
	if resp != nil {
		// 1. Retry-After header (seconds or HTTP-date)
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs)*time.Second + time.Second
			}
		}
		// 2. X-RateLimit-Reset-After header
		if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs)*time.Second + time.Second
			}
		}
	}
	// 3. Body: "reset after 18h31m10s" / "reset after 39s"
	if m := httpResetAfterRe.FindStringSubmatch(errorText); len(m) >= 4 {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		total := time.Duration(h)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second))
		if total > 0 {
			return total + time.Second
		}
	}
	// 4. Body: "Please retry in Xs" or "Please retry in Xms"
	if m := httpRetryInRe.FindStringSubmatch(errorText); len(m) >= 3 {
		val, _ := strconv.ParseFloat(m[1], 64)
		if val > 0 {
			if strings.ToLower(m[2]) == "ms" {
				return time.Duration(val*float64(time.Millisecond)) + time.Second
			}
			return time.Duration(val*float64(time.Second)) + time.Second
		}
	}
	// 5. Body JSON: "retryDelay": "34.074824224s"
	if m := httpRetryDelayJSONRe.FindStringSubmatch(errorText); len(m) >= 3 {
		val, _ := strconv.ParseFloat(m[1], 64)
		if val > 0 {
			if strings.ToLower(m[2]) == "ms" {
				return time.Duration(val*float64(time.Millisecond)) + time.Second
			}
			return time.Duration(val*float64(time.Second)) + time.Second
		}
	}
	return 0
}

var (
	httpResetAfterRe     = regexp.MustCompile(`(?i)reset after (?:(\d+)h)?(?:(\d+)m)?(\d+(?:\.\d+)?)s`)
	httpRetryInRe        = regexp.MustCompile(`(?i)Please retry in ([0-9.]+)(ms|s)`)
	httpRetryDelayJSONRe = regexp.MustCompile(`(?i)"retryDelay"\s*:\s*"([0-9.]+)(ms|s)"`)
)

func (g *GeminiOAuthProvider) currentEndpointCandidates() []string {
	g.endpointMu.RLock()
	defer g.endpointMu.RUnlock()
	if len(g.endpointCandidates) == 0 {
		return []string{codeAssistPrimaryEndpoint, codeAssistSandboxEndpoint}
	}
	return append([]string(nil), g.endpointCandidates...)
}

func (g *GeminiOAuthProvider) setEffectiveEndpoint(endpoint string) {
	g.endpointMu.Lock()
	defer g.endpointMu.Unlock()
	g.effectiveEndpoint = endpoint
}

// extractRateLimitHeaders builds a bracketed string of rate-limit headers from
// the HTTP response. The format is designed to be parsed by parseResetDelay()
// in the provider pool layer (e.g., "[retry-after:39] [x-ratelimit-reset-after:45]").
func extractRateLimitHeaders(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	var sb strings.Builder
	if v := resp.Header.Get("Retry-After"); v != "" {
		sb.WriteString(fmt.Sprintf(" [retry-after:%s]", v))
	}
	if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
		sb.WriteString(fmt.Sprintf(" [x-ratelimit-reset-after:%s]", v))
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		sb.WriteString(fmt.Sprintf(" [x-ratelimit-reset:%s]", v))
	}
	return sb.String()
}
