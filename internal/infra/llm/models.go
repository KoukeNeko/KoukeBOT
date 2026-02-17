package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/genai"
)

// ModelEntry describes a model returned by the API.
type ModelEntry struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	ProviderID         string `json:"provider_id,omitempty"`
	SupportsText       bool   `json:"supports_text"`
	SupportsMultimodal bool   `json:"supports_multimodal"`
	SupportsTools      bool   `json:"supports_tools"`
	Reasoning          bool   `json:"reasoning"`
}

// ListGeminiModels fetches available models from the Gemini API.
// It tries API key first, then OAuth token. If neither is available,
// returns a curated default list.
func ListGeminiModels(ctx context.Context, apiKey string, oauthToken *OAuthTokenData) ([]ModelEntry, error) {
	return ListGeminiModelsForProvider(ctx, apiKey, oauthToken, "")
}

// ListGeminiModelsForProvider fetches available models while respecting the
// selected provider type. When providerType is OAuth, API-key listing is skipped.
func ListGeminiModelsForProvider(ctx context.Context, apiKey string, oauthToken *OAuthTokenData, providerType string) ([]ModelEntry, error) {
	providerID := NormalizeGoogleProviderID(providerType)
	switch providerID {
	case ProviderGoogleGeminiCLI:
		return providerCatalogForGeminiCLI(), nil
	case ProviderGoogleAntigravity:
		return providerCatalogForAntigravity(), nil
	}

	client, err := createListingClient(ctx, apiKey, oauthToken, providerType)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return defaultModelsWithCapabilities(), nil
	}

	return fetchModelsFromAPI(ctx, client)
}

func createListingClient(ctx context.Context, apiKey string, oauthToken *OAuthTokenData, providerType string) (*genai.Client, error) {
	if strings.EqualFold(strings.TrimSpace(providerType), "oauth") {
		// Cloud Code Assist API has no model listing endpoint.
		return nil, nil
	}

	if apiKey != "" {
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return nil, fmt.Errorf("create API key client: %w", err)
		}
		return client, nil
	}

	// Cloud Code Assist API has no model listing endpoint.
	// Return nil to fall back to the curated default list.
	if oauthToken != nil && oauthToken.AccessToken != "" {
		return nil, nil
	}

	return nil, nil
}

func fetchModelsFromAPI(ctx context.Context, client *genai.Client) ([]ModelEntry, error) {
	var models []ModelEntry

	for m, err := range client.Models.All(ctx) {
		if err != nil {
			if len(models) == 0 {
				return defaultModelsWithCapabilities(), nil
			}
			break
		}

		// Only include generateContent-capable models.
		if !supportsGeneration(m) {
			continue
		}

		displayName := m.DisplayName
		if displayName == "" {
			displayName = m.Name
		}

		models = append(models, ModelEntry{
			ID:                 NormalizeModelID(m.Name),
			DisplayName:        displayName,
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          strings.Contains(strings.ToLower(m.Name), "thinking"),
		})
	}

	if len(models) == 0 {
		return defaultModelsWithCapabilities(), nil
	}

	// Sort: newer/larger models first.
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID > models[j].ID
	})

	return normalizeModelEntries(models), nil
}

func supportsGeneration(m *genai.Model) bool {
	for _, action := range m.SupportedActions {
		if strings.Contains(action, "generateContent") {
			return true
		}
	}
	return false
}

func defaultModels() []ModelEntry {
	return defaultModelsWithCapabilities()
}

func defaultModelsWithCapabilities() []ModelEntry {
	return normalizeModelEntries([]ModelEntry{
		{
			ID:                 "gemini-3-pro-preview",
			DisplayName:        "Gemini 3 Pro (Preview)",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 "gemini-3-flash-preview",
			DisplayName:        "Gemini 3 Flash (Preview)",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 "gemini-2.5-pro",
			DisplayName:        "Gemini 2.5 Pro",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 "gemini-2.5-flash",
			DisplayName:        "Gemini 2.5 Flash",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 "gemini-2.0-flash",
			DisplayName:        "Gemini 2.0 Flash",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          false,
		},
		{
			ID:                 "gemini-2.0-flash-lite",
			DisplayName:        "Gemini 2.0 Flash Lite",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      true,
			Reasoning:          false,
		},
		{
			ID:                 "gemini-1.5-pro",
			DisplayName:        "Gemini 1.5 Pro",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          false,
		},
		{
			ID:                 "gemini-1.5-flash",
			DisplayName:        "Gemini 1.5 Flash",
			ProviderID:         ProviderGeminiAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          false,
		},
	})
}

func normalizeModelEntries(entries []ModelEntry) []ModelEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]ModelEntry, 0, len(entries))
	for _, entry := range entries {
		modelID := normalizeReturnedModelID(entry.ProviderID, entry.ID)
		out = append(out, ModelEntry{
			ID:                 modelID,
			DisplayName:        entry.DisplayName,
			ProviderID:         entry.ProviderID,
			SupportsText:       entry.SupportsText,
			SupportsMultimodal: entry.SupportsMultimodal,
			SupportsTools:      entry.SupportsTools,
			Reasoning:          entry.Reasoning,
		})
	}
	return out
}

func normalizeReturnedModelID(providerID, modelID string) string {
	if pid, mid := SplitProviderModelID(modelID); mid != "" {
		return BuildProviderModelID(pid, mid)
	}
	mid := NormalizeGoogleModelAlias(modelID)
	if providerID == ProviderGoogleGeminiCLI || providerID == ProviderGoogleAntigravity {
		return BuildProviderModelID(providerID, mid)
	}
	return NormalizeModelID(mid)
}

func providerCatalogForGeminiCLI() []ModelEntry {
	return normalizeModelEntries([]ModelEntry{
		{
			ID:                 BuildProviderModelID(ProviderGoogleGeminiCLI, "gemini-3-pro-preview"),
			DisplayName:        "Google Gemini CLI / Gemini 3 Pro (Preview)",
			ProviderID:         ProviderGoogleGeminiCLI,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      false,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderGoogleGeminiCLI, "gemini-3-flash-preview"),
			DisplayName:        "Google Gemini CLI / Gemini 3 Flash (Preview)",
			ProviderID:         ProviderGoogleGeminiCLI,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      false,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderGoogleGeminiCLI, "gemini-2.5-pro"),
			DisplayName:        "Google Gemini CLI / Gemini 2.5 Pro",
			ProviderID:         ProviderGoogleGeminiCLI,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      false,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderGoogleGeminiCLI, "gemini-2.5-flash"),
			DisplayName:        "Google Gemini CLI / Gemini 2.5 Flash",
			ProviderID:         ProviderGoogleGeminiCLI,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      false,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderGoogleGeminiCLI, "gemini-2.0-flash"),
			DisplayName:        "Google Gemini CLI / Gemini 2.0 Flash",
			ProviderID:         ProviderGoogleGeminiCLI,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      false,
			Reasoning:          false,
		},
	})
}

func providerCatalogForAntigravity() []ModelEntry {
	return normalizeModelEntries([]ModelEntry{
		{
			ID:                 BuildProviderModelID(ProviderGoogleAntigravity, "claude-opus-4-6-thinking"),
			DisplayName:        "Google Antigravity / Claude Opus 4.6 Thinking",
			ProviderID:         ProviderGoogleAntigravity,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderGoogleAntigravity, "claude-sonnet-4-thinking"),
			DisplayName:        "Google Antigravity / Claude Sonnet 4 Thinking",
			ProviderID:         ProviderGoogleAntigravity,
			SupportsText:       true,
			SupportsMultimodal: false,
			SupportsTools:      true,
			Reasoning:          true,
		},
	})
}
