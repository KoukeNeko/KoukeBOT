package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAIStudioSessionStoreSaveLoadDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aistudio_session.json")
	session := &AIStudioSession{
		APIKey:             "api-key",
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}

	if err := SaveAIStudioSession(path, session); err != nil {
		t.Fatalf("SaveAIStudioSession() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat session file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("session file perm = %o, want %o", got, 0o600)
	}

	loaded, err := LoadAIStudioSession(path)
	if err != nil {
		t.Fatalf("LoadAIStudioSession() error = %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded session is nil")
	}
	if loaded.APIKey != "api-key" {
		t.Fatalf("APIKey = %q, want %q", loaded.APIKey, "api-key")
	}
	if loaded.Endpoint == "" {
		t.Fatalf("Endpoint should be normalized with default value")
	}
	if loaded.XGoogExt519733851 != defaultAIStudioGoogExt519733851 {
		t.Fatalf("XGoogExt519733851 = %q, want %q", loaded.XGoogExt519733851, defaultAIStudioGoogExt519733851)
	}

	if err := DeleteAIStudioSession(path); err != nil {
		t.Fatalf("DeleteAIStudioSession() error = %v", err)
	}

	loadedAfterDelete, err := LoadAIStudioSession(path)
	if err != nil {
		t.Fatalf("LoadAIStudioSession() after delete error = %v", err)
	}
	if loadedAfterDelete != nil {
		t.Fatalf("expected nil session after delete")
	}
}

func TestAIStudioSessionStoreDefaultsAPIKeyAndGoogExt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aistudio_session.json")
	session := &AIStudioSession{
		XBrowserValidation: "validation",
		XClientData:        "client-data",
		XGoogAuthUser:      "0",
		UserAgent:          "Mozilla/test",
	}

	if err := SaveAIStudioSession(path, session); err != nil {
		t.Fatalf("SaveAIStudioSession() with defaults error = %v", err)
	}

	loaded, err := LoadAIStudioSession(path)
	if err != nil {
		t.Fatalf("LoadAIStudioSession() error = %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded session")
	}
	if loaded.APIKey != defaultAIStudioAPIKey {
		t.Fatalf("APIKey = %q, want default %q", loaded.APIKey, defaultAIStudioAPIKey)
	}
	if loaded.XGoogExt519733851 != defaultAIStudioGoogExt519733851 {
		t.Fatalf("XGoogExt519733851 = %q, want default %q", loaded.XGoogExt519733851, defaultAIStudioGoogExt519733851)
	}
}

func TestAIStudioSessionStoreValidatesRequiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aistudio_session.json")
	session := &AIStudioSession{
		APIKey:        "api-key",
		XClientData:   "client-data",
		XGoogAuthUser: "0",
		UserAgent:     "Mozilla/test",
	}

	if err := SaveAIStudioSession(path, session); err == nil {
		t.Fatal("expected validation error for missing x_browser_validation")
	}
}
