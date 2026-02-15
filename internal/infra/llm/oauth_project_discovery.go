package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func codeAssistHeaders(accessToken string) map[string]string {
	return map[string]string{
		"Authorization":     "Bearer " + accessToken,
		"Content-Type":      "application/json",
		"User-Agent":        "google-api-nodejs-client/9.15.1",
		"X-Goog-Api-Client": "gl-node/koukeBOT",
	}
}

func discoverProject(ctx context.Context, accessToken string) (string, error) {
	headers := codeAssistHeaders(accessToken)
	projectID, err := loadCodeAssist(ctx, headers)
	if err == nil && projectID != "" {
		return projectID, nil
	}
	logFlow.Logf("loadCodeAssist: %v, trying onboardUser...", err)
	return onboardUser(ctx, headers)
}

func loadCodeAssist(ctx context.Context, headers map[string]string) (string, error) {
	body := `{"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	respBody, err := doCodeAssistRequest(ctx, codeAssistURL+"/v1internal:loadCodeAssist", body, headers)
	if err != nil {
		return "", err
	}

	var result struct {
		CloudAICompanionProject any `json:"cloudaicompanionProject"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	return extractProjectID(result.CloudAICompanionProject), nil
}

func onboardUser(ctx context.Context, headers map[string]string) (string, error) {
	body := `{"tierId":"legacy-tier","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	respBody, err := doCodeAssistRequest(ctx, codeAssistURL+"/v1internal:onboardUser", body, headers)
	if err != nil {
		return "", err
	}

	var lro struct {
		Done     bool   `json:"done"`
		Name     string `json:"name"`
		Response struct {
			CloudAICompanionProject struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		} `json:"response"`
	}
	if err := json.Unmarshal(respBody, &lro); err != nil {
		return "", fmt.Errorf("parse onboardUser response: %w", err)
	}

	if lro.Done {
		return lro.Response.CloudAICompanionProject.ID, nil
	}
	if lro.Name == "" {
		return "", errors.New("onboardUser returned no operation name")
	}

	return pollOperation(ctx, lro.Name, headers)
}

func pollOperation(ctx context.Context, operationName string, headers map[string]string) (string, error) {
	for attempt := 0; attempt < pollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		reqURL := codeAssistURL + "/v1internal/" + operationName
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
			} `json:"response"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err := json.Unmarshal(body, &lro); err != nil {
			continue
		}
		if lro.Done {
			return lro.Response.CloudAICompanionProject.ID, nil
		}
	}

	return "", errors.New("operation polling timeout")
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

// extractProjectID handles both string and object forms of cloudaicompanionProject.
func extractProjectID(v any) string {
	switch p := v.(type) {
	case string:
		return p
	case map[string]any:
		if id, ok := p["id"].(string); ok {
			return id
		}
	}
	return ""
}
