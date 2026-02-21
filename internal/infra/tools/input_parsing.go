package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// readString extracts a trimmed string value from a tool-input map.
// Handles string, json.Number, and other types via fmt.Sprintf fallback.
func readString(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	v, ok := input[key]
	if !ok {
		return ""
	}
	switch raw := v.(type) {
	case string:
		return strings.TrimSpace(raw)
	case json.Number:
		return strings.TrimSpace(raw.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

// readBool extracts a boolean value from a tool-input map.
// Accepts native bools and the string "true" (case-insensitive).
func readBool(input map[string]interface{}, key string) bool {
	if input == nil {
		return false
	}
	raw, ok := input[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

// readInt64 extracts an int64 value from a tool-input map.
// Handles int, int32, int64, float64, json.Number, and numeric strings.
func readInt64(input map[string]interface{}, key string) (int64, error) {
	if input == nil {
		return 0, fmt.Errorf("missing key")
	}
	raw, ok := input[key]
	if !ok {
		return 0, fmt.Errorf("missing key")
	}
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, err
		}
		return n, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("invalid number type")
	}
}

// readMapString extracts a string from a map using fmt.Sprintf fallback.
// Simpler than readString — does not handle json.Number specially.
func readMapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

// parseStringSlice converts a raw interface{} ([]string or []interface{})
// into a deduplicated, non-empty string slice.
func parseStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return uniqueNonEmptyStrings(v)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return uniqueNonEmptyStrings(out)
	default:
		return nil
	}
}

// uniqueNonEmptyStrings deduplicates a string slice and removes empty entries.
func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
