package app

import (
	"strings"
	"testing"
)

func TestCompressToolResultKeepsWebFetchMetadata(t *testing.T) {
	content := "URL: https://example.com\nTitle: Example\n--- CONTENT START ---\n" +
		strings.Repeat("A", 9000)

	compressed := compressToolResult("web_fetch", content)

	if !strings.Contains(compressed, "URL: https://example.com") {
		t.Fatalf("compressed result must keep source URL metadata")
	}
	if len(compressed) >= len(content) {
		t.Fatalf("compressed content should be shorter than original")
	}
	if !strings.Contains(compressed, "truncated for token efficiency") {
		t.Fatalf("compressed output should include truncation marker")
	}
}
