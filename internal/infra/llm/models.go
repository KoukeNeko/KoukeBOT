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
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// ListGeminiModels fetches available models from the Gemini API.
// It tries API key first, then OAuth token. If neither is available,
// returns a curated default list.
func ListGeminiModels(ctx context.Context, apiKey string, oauthToken *OAuthTokenData) ([]ModelEntry, error) {
	client, err := createListingClient(ctx, apiKey, oauthToken)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return defaultModels(), nil
	}

	return fetchModelsFromAPI(ctx, client)
}

func createListingClient(ctx context.Context, apiKey string, oauthToken *OAuthTokenData) (*genai.Client, error) {
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
				return defaultModels(), nil
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
			ID:          m.Name,
			DisplayName: displayName,
		})
	}

	if len(models) == 0 {
		return defaultModels(), nil
	}

	// Sort: newer/larger models first.
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID > models[j].ID
	})

	return models, nil
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
	return []ModelEntry{
		{ID: "gemini-3-pro-preview", DisplayName: "Gemini 3 Pro (Preview)"},
		{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash (Preview)"},
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro"},
		{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash"},
		{ID: "gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash"},
		{ID: "gemini-2.0-flash-lite", DisplayName: "Gemini 2.0 Flash Lite"},
		{ID: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro"},
		{ID: "gemini-1.5-flash", DisplayName: "Gemini 1.5 Flash"},
	}
}
