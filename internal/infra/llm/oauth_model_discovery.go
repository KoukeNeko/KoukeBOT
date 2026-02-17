package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const fetchAvailableModelsPath = "/v1internal:fetchAvailableModels"

// fetchAvailableModelsRequest is the request body for /v1internal:fetchAvailableModels.
type fetchAvailableModelsRequest struct {
	Project string `json:"project,omitempty"`
}

// fetchAvailableModelsResponse holds the response from Cloud Code Assist API.
type fetchAvailableModelsResponse struct {
	Models map[string]fetchAvailableModelInfo `json:"models,omitempty"`
}

// fetchAvailableModelInfo describes a single model returned by the API.
type fetchAvailableModelInfo struct {
	DisplayName string                  `json:"displayName,omitempty"`
	QuotaInfo   *fetchAvailableQuota    `json:"quotaInfo,omitempty"`
}

// fetchAvailableQuota contains quota information for a model.
type fetchAvailableQuota struct {
	RemainingFraction float64 `json:"remainingFraction,omitempty"`
	ResetTime         string  `json:"resetTime,omitempty"`
	IsExhausted       bool    `json:"isExhausted,omitempty"`
}

// FetchAvailableModels calls /v1internal:fetchAvailableModels on the Cloud Code
// Assist API to dynamically discover which models are available for the user's
// project. Returns nil on failure so callers can fall back to static catalogs.
func FetchAvailableModels(ctx context.Context, tokenData *OAuthTokenData, providerID string) ([]ModelEntry, error) {
	if tokenData == nil || tokenData.AccessToken == "" {
		return nil, fmt.Errorf("no OAuth token available for model discovery")
	}

	httpClient := oauth2.NewClient(ctx, NewTokenSource(ctx, tokenData))
	spec := googleProviderSpec(providerID)
	endpoints := resolveOAuthEndpointCandidates(spec.EndpointCandidates, EndpointPolicyPrimaryThenSandbox)

	reqBody := fetchAvailableModelsRequest{}
	if tokenData.ProjectID != "" {
		reqBody.Project = tokenData.ProjectID
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var lastErr error
	for _, endpoint := range endpoints {
		models, err := fetchModelsFromEndpoint(discoveryCtx, httpClient, endpoint, bodyBytes, providerID)
		if err == nil {
			return models, nil
		}
		lastErr = err
		logOAuth.Warnf("fetchAvailableModels endpoint=%s: %v", endpoint, err)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no endpoint candidates for fetchAvailableModels")
	}
	return nil, lastErr
}

func fetchModelsFromEndpoint(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	body []byte,
	providerID string,
) ([]ModelEntry, error) {
	reqURL := strings.TrimRight(endpoint, "/") + fetchAvailableModelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result fetchAvailableModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return convertDiscoveredModels(result, providerID), nil
}

// convertDiscoveredModels transforms the API response into ModelEntry slice.
func convertDiscoveredModels(resp fetchAvailableModelsResponse, providerID string) []ModelEntry {
	if len(resp.Models) == 0 {
		return nil
	}

	entries := make([]ModelEntry, 0, len(resp.Models))
	for modelID, info := range resp.Models {
		// Skip internal models (chat completion, tab completion, etc.)
		lowerID := strings.ToLower(modelID)
		if strings.Contains(lowerID, "chat_") || strings.Contains(lowerID, "tab_") {
			continue
		}

		// Skip exhausted models
		if info.QuotaInfo != nil && info.QuotaInfo.IsExhausted {
			continue
		}

		displayName := info.DisplayName
		if displayName == "" {
			displayName = modelID
		}

		// Infer provider based on model name pattern
		targetProvider := providerID
		if targetProvider == "" {
			targetProvider = inferProviderFromModelName(modelID)
		}

		entries = append(entries, ModelEntry{
			ID:                 BuildProviderModelID(targetProvider, modelID),
			DisplayName:        fmt.Sprintf("%s / %s", googleProviderSpec(targetProvider).Label, displayName),
			ProviderID:         targetProvider,
			SupportsText:       true,
			SupportsMultimodal: inferMultimodalSupport(modelID),
			SupportsTools:      true,
			Reasoning:          inferReasoningSupport(modelID),
		})
	}

	// Sort by model ID for stable ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	return entries
}

func inferProviderFromModelName(modelID string) string {
	lower := strings.ToLower(modelID)
	if strings.Contains(lower, "claude") {
		return ProviderGoogleAntigravity
	}
	return ProviderGoogleGeminiCLI
}

func inferMultimodalSupport(modelID string) bool {
	lower := strings.ToLower(modelID)
	// Claude models through Cloud Code Assist don't support multimodal
	if strings.Contains(lower, "claude") {
		return false
	}
	// Gemini 2.0+ supports multimodal
	return true
}

func inferReasoningSupport(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "thinking") ||
		strings.Contains(lower, "2.5") ||
		strings.Contains(lower, "3")
}
