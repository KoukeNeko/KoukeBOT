package mcp

import (
	"strings"
	"testing"
)

func TestSanitizeHeaderMap(t *testing.T) {
	values := map[string]string{
		"Authorization": "Bearer abc123",
		"X-API-Key":     "secret-key",
		"X-Trace-ID":    "trace-1",
	}

	got := sanitizeHeaderMap(values)
	if got["Authorization"] != redactedValue {
		t.Fatalf("authorization should be redacted, got %q", got["Authorization"])
	}
	if got["X-API-Key"] != redactedValue {
		t.Fatalf("x-api-key should be redacted, got %q", got["X-API-Key"])
	}
	if got["X-Trace-ID"] != "trace-1" {
		t.Fatalf("x-trace-id should keep original value")
	}
}

func TestSanitizeJSONBytes(t *testing.T) {
	raw := []byte(`{"token":"abc","nested":{"password":"123","ok":"y"},"items":[{"apiKey":"k1"}]}`)
	got := string(sanitizeJSONBytes(raw))

	for _, key := range []string{"abc", "123", "k1"} {
		if strings.Contains(got, key) {
			t.Fatalf("sanitized payload should not contain %q: %s", key, got)
		}
	}
	if !strings.Contains(got, redactedValue) {
		t.Fatalf("sanitized payload should contain redacted marker: %s", got)
	}
	if !strings.Contains(got, `"ok":"y"`) {
		t.Fatalf("non-sensitive fields should be preserved: %s", got)
	}
}
