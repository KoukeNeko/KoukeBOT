package mcp

import (
	"encoding/json"
	"strings"
)

const redactedValue = "***REDACTED***"

var sensitiveKeyTokens = []string{
	"authorization",
	"proxy-authorization",
	"api-key",
	"apikey",
	"token",
	"secret",
	"password",
	"cookie",
	"set-cookie",
}

func sanitizeHeaderMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if isSensitiveKey(key) && strings.TrimSpace(value) != "" {
			out[key] = redactedValue
			continue
		}
		out[key] = value
	}
	return out
}

func sanitizeHeaderValues(values map[string][]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, list := range values {
		value := strings.Join(list, ", ")
		if isSensitiveKey(key) && strings.TrimSpace(value) != "" {
			out[key] = redactedValue
			continue
		}
		out[key] = value
	}
	return out
}

func sanitizeJSONBytes(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return payload
	}
	redacted := redactJSONValue(decoded, "")
	out, err := json.Marshal(redacted)
	if err != nil {
		return payload
	}
	return out
}

func sanitizePayloadText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	return string(sanitizeJSONBytes([]byte(text)))
}

func redactJSONValue(value any, parentKey string) any {
	if isSensitiveKey(parentKey) {
		return redactedValue
	}

	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			out[key] = redactJSONValue(entry, key)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for idx, entry := range typed {
			out[idx] = redactJSONValue(entry, parentKey)
		}
		return out
	default:
		return typed
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, token := range sensitiveKeyTokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
