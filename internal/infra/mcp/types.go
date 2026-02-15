package mcp

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

var sanitizeToolNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9_]`)

type ToolDescriptor struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type ServerStatus struct {
	ServerID        string    `json:"server_id"`
	Enabled         bool      `json:"enabled"`
	Transport       string    `json:"transport"`
	Reachable       bool      `json:"reachable"`
	LastError       string    `json:"last_error,omitempty"`
	DiscoveredTools []string  `json:"discovered_tools"`
	RegisteredTools []string  `json:"registered_tools"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func ExposedToolName(serverID, toolName string) string {
	return "mcp." + sanitizeToolToken(serverID) + "." + sanitizeToolToken(toolName)
}

func sanitizeToolToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	sanitized := sanitizeToolNameRegexp.ReplaceAllString(trimmed, "_")
	sanitized = strings.Trim(sanitized, "_")
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}

func sortStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
}
