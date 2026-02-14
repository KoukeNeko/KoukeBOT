package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectFileToolPersonaIDPrefersConfiguredPersona(t *testing.T) {
	baseDir := t.TempDir()
	mustMkdir(t, filepath.Join(baseDir, "_template"))
	mustMkdir(t, filepath.Join(baseDir, "satsuki"))
	mustMkdir(t, filepath.Join(baseDir, "hayasaka"))

	got := selectFileToolPersonaID(baseDir, "satsuki")
	if got != "satsuki" {
		t.Fatalf("selectFileToolPersonaID() = %q, want %q", got, "satsuki")
	}
}

func TestSelectFileToolPersonaIDSkipsTemplateFallback(t *testing.T) {
	baseDir := t.TempDir()
	mustMkdir(t, filepath.Join(baseDir, "_template"))
	mustMkdir(t, filepath.Join(baseDir, "jjw"))

	got := selectFileToolPersonaID(baseDir, "")
	if got != "jjw" {
		t.Fatalf("selectFileToolPersonaID() = %q, want %q", got, "jjw")
	}
}

func TestSelectFileToolPersonaIDReturnsEmptyWhenNoPersona(t *testing.T) {
	baseDir := t.TempDir()
	mustMkdir(t, filepath.Join(baseDir, "_template"))

	got := selectFileToolPersonaID(baseDir, "missing")
	if got != "" {
		t.Fatalf("selectFileToolPersonaID() = %q, want empty string", got)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}
