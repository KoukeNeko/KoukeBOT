package llm

import (
	"context"
	"strings"
	"testing"
)

func TestCreateListingClientSkipsAPIKeyWhenProviderTypeOAuth(t *testing.T) {
	client, err := createListingClient(context.Background(), "fake-api-key", nil, "oauth")
	if err != nil {
		t.Fatalf("createListingClient returned error: %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client for oauth provider type")
	}
}

func TestListGeminiModelsForProviderNormalizesModelIDs(t *testing.T) {
	models, err := ListGeminiModelsForProvider(context.Background(), "fake-api-key", nil, "oauth")
	if err != nil {
		t.Fatalf("ListGeminiModelsForProvider returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("expected non-empty model list")
	}
	for _, model := range models {
		if strings.HasPrefix(model.ID, "models/") {
			t.Fatalf("model id should be normalized, got %q", model.ID)
		}
	}
}

func TestProviderCatalogForAntigravityExcludesDeprecatedClaudeOpus46(t *testing.T) {
	models := staticCatalogForProvider(ProviderGoogleAntigravity)
	if len(models) == 0 {
		t.Fatalf("expected non-empty antigravity catalog")
	}
	for _, model := range models {
		if strings.HasSuffix(model.ID, "/claude-opus-4-6") {
			t.Fatalf("deprecated model should not be listed: %q", model.ID)
		}
	}
}
