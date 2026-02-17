package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func codeAssistHeaders(accessToken string) map[string]string {
	return map[string]string{
		"Authorization":     "Bearer " + accessToken,
		"Content-Type":      "application/json",
		"User-Agent":        "google-cloud-sdk vscode_cloudshelleditor/0.1",
		"X-Goog-Api-Client": "google-cloud-sdk vscode_cloudshelleditor/0.1",
		"Client-Metadata":   `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`,
	}
}

func discoverProject(ctx context.Context, accessToken string) (string, error) {
	projectID, _, _, err := discoverProjectWithProvider(
		ctx,
		accessToken,
		ProviderGoogleGeminiCLI,
		EndpointPolicyPrimaryThenSandbox,
	)
	return projectID, err
}

func discoverProjectWithProvider(
	ctx context.Context,
	accessToken string,
	providerID string,
	endpointPolicy string,
) (projectID string, tierID string, endpointUsed string, err error) {
	headers := codeAssistHeaders(accessToken)
	spec := googleProviderSpec(providerID)
	endpoints := resolveOAuthEndpointCandidates(spec.EndpointCandidates, endpointPolicy)
	var lastErr error
	for _, endpoint := range endpoints {
		projectID, tierID, err := loadCodeAssist(ctx, endpoint, headers)
		if err == nil && projectID != "" {
			return projectID, tierID, endpoint, nil
		}
		logFlow.Logf("loadCodeAssist endpoint=%s: %v, trying onboardUser...", endpoint, err)

		projectID, tierID, err = onboardUser(ctx, endpoint, headers)
		if err == nil && projectID != "" {
			return projectID, tierID, endpoint, nil
		}
		lastErr = err
		logFlow.Warnf("onboardUser endpoint=%s failed: %v", endpoint, err)
	}

	if project := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")); project != "" {
		return project, "manual-project", firstEndpointOrDefault(endpoints), nil
	}
	if project := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT_ID")); project != "" {
		return project, "manual-project", firstEndpointOrDefault(endpoints), nil
	}

	if lastErr == nil {
		lastErr = errors.New("project discovery failed")
	}
	if strings.Contains(strings.ToLower(lastErr.Error()), "vpc_sc") {
		return "", "", "", fmt.Errorf(
			"project discovery blocked by VPC_SC; set GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID: %w",
			lastErr,
		)
	}
	return "", "", "", lastErr
}

func loadCodeAssist(ctx context.Context, endpoint string, headers map[string]string) (string, string, error) {
	body := `{"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	respBody, err := doCodeAssistRequest(ctx, strings.TrimRight(endpoint, "/")+"/v1internal:loadCodeAssist", body, headers)
	if err != nil {
		return "", "", err
	}

	var result struct {
		CloudAICompanionProject any `json:"cloudaicompanionProject"`
		CurrentTier             any `json:"currentTier"`
		AllowedTiers            []struct {
			ID string `json:"id"`
		} `json:"allowedTiers"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	tierID := extractStringOrID(result.CurrentTier)
	if tierID == "" && len(result.AllowedTiers) > 0 {
		tierID = strings.TrimSpace(result.AllowedTiers[0].ID)
	}
	return extractProjectID(result.CloudAICompanionProject), tierID, nil
}

func onboardUser(ctx context.Context, endpoint string, headers map[string]string) (string, string, error) {
	body := `{"tierId":"legacy-tier","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	respBody, err := doCodeAssistRequest(ctx, strings.TrimRight(endpoint, "/")+"/v1internal:onboardUser", body, headers)
	if err != nil {
		return "", "", err
	}

	var lro struct {
		Done     bool   `json:"done"`
		Name     string `json:"name"`
		Response struct {
			CloudAICompanionProject struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
			TierID string `json:"tierId"`
		} `json:"response"`
	}
	if err := json.Unmarshal(respBody, &lro); err != nil {
		return "", "", fmt.Errorf("parse onboardUser response: %w", err)
	}

	if lro.Done {
		tierID := strings.TrimSpace(lro.Response.TierID)
		if tierID == "" {
			tierID = "legacy-tier"
		}
		return lro.Response.CloudAICompanionProject.ID, tierID, nil
	}
	if lro.Name == "" {
		return "", "", errors.New("onboardUser returned no operation name")
	}

	return pollOperation(ctx, endpoint, lro.Name, headers)
}

func pollOperation(ctx context.Context, endpoint, operationName string, headers map[string]string) (string, string, error) {
	for attempt := 0; attempt < pollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(pollInterval):
		}

		reqURL := strings.TrimRight(endpoint, "/") + "/v1internal/" + operationName
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := oauthHTTPClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var lro struct {
			Done     bool `json:"done"`
			Response struct {
				CloudAICompanionProject struct {
					ID string `json:"id"`
				} `json:"cloudaicompanionProject"`
				TierID string `json:"tierId"`
			} `json:"response"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err := json.Unmarshal(body, &lro); err != nil {
			continue
		}
		if lro.Done {
			tierID := strings.TrimSpace(lro.Response.TierID)
			if tierID == "" {
				tierID = "legacy-tier"
			}
			return lro.Response.CloudAICompanionProject.ID, tierID, nil
		}
	}

	return "", "", errors.New("operation polling timeout")
}

func doCodeAssistRequest(ctx context.Context, requestURL, body string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Code Assist API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func firstEndpointOrDefault(endpoints []string) string {
	if len(endpoints) == 0 {
		return codeAssistPrimaryEndpoint
	}
	return endpoints[0]
}

// extractProjectID handles both string and object forms of cloudaicompanionProject.
func extractProjectID(v any) string {
	return extractStringOrID(v)
}

// extractStringOrID handles API fields that may be either a plain string or
// an object with an "id" field (e.g., currentTier, cloudaicompanionProject).
func extractStringOrID(v any) string {
	switch p := v.(type) {
	case string:
		return strings.TrimSpace(p)
	case map[string]any:
		if id, ok := p["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}
