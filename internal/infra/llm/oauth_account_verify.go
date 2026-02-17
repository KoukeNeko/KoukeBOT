package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

const defaultOAuthVerifyModel = "gemini-2.5-pro"

// OAuthAccountVerifyResult is the result of probing one OAuth account.
type OAuthAccountVerifyResult struct {
	Email        string
	ProjectID    string
	ProviderID   string
	TierID       string
	EndpointUsed string
	Latency      time.Duration
	Preview      string
}

// VerifyOAuthAccount validates one OAuth account end-to-end:
// token refresh -> project discovery -> minimal generation request.
func VerifyOAuthAccount(ctx context.Context, tokenData *OAuthTokenData, model string) (*OAuthAccountVerifyResult, error) {
	return VerifyOAuthAccountWithProvider(
		ctx,
		tokenData,
		tokenData.ProviderID,
		model,
		EndpointPolicyPrimaryThenSandbox,
	)
}

// VerifyOAuthAccountWithProvider validates one OAuth account end-to-end with explicit provider routing.
func VerifyOAuthAccountWithProvider(
	ctx context.Context,
	tokenData *OAuthTokenData,
	providerID string,
	model string,
	endpointPolicy string,
) (*OAuthAccountVerifyResult, error) {
	if tokenData == nil {
		return nil, errors.New("missing OAuth token data")
	}
	if strings.TrimSpace(tokenData.RefreshToken) == "" {
		return nil, errors.New("missing refresh_token")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(model) == "" {
		model = defaultOAuthVerifyModel
	}
	providerID = NormalizeGoogleProviderID(firstNonEmptyString(providerID, tokenData.ProviderID))
	if providerID == "" {
		providerID = ProviderGoogleGeminiCLI
	}
	endpointPolicy = ResolveEndpointPolicy(endpointPolicy)

	if err := RefreshIfNeeded(ctx, tokenData, ""); err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	if strings.TrimSpace(tokenData.AccessToken) == "" {
		return nil, errors.New("missing access_token")
	}

	if email, err := fetchUserEmail(ctx, tokenData.AccessToken); err == nil && strings.TrimSpace(email) != "" {
		tokenData.Email = strings.TrimSpace(email)
	}

	projectID, tierID, endpointUsed, err := discoverProjectWithProvider(
		ctx,
		tokenData.AccessToken,
		providerID,
		endpointPolicy,
	)
	if err != nil {
		return nil, fmt.Errorf("project discovery: %w", err)
	}
	tokenData.ProjectID = strings.TrimSpace(projectID)
	tokenData.ProviderID = providerID
	tokenData.TierID = strings.TrimSpace(tierID)
	tokenData.EndpointUsed = strings.TrimSpace(endpointUsed)
	if tokenData.ProjectID == "" {
		return nil, errors.New("project discovery returned empty project id")
	}

	provider, err := NewGeminiOAuthProviderWithProvider(
		ctx,
		tokenData,
		providerID,
		model,
		endpointPolicy,
	)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	probeCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		probeCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
	}
	defer cancel()

	start := time.Now()
	resp, err := provider.Chat(probeCtx, ChatRequest{
		SystemPrompt: `You are a health probe. Reply with exactly "OK".`,
		Messages: []domain.Message{
			{
				Role:    domain.RoleUser,
				Content: "health-check",
			},
		},
		Generation: domain.GenerationConfig{
			Temperature: 0.1,
			TopP:        0.1,
		},
		ToolMode: ToolModeDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("probe request: %w", err)
	}

	preview := strings.TrimSpace(resp.Content)
	if preview == "" {
		return nil, errors.New("probe request returned empty content")
	}
	if len(preview) > 96 {
		preview = preview[:96] + "..."
	}

	return &OAuthAccountVerifyResult{
		Email:        strings.TrimSpace(tokenData.Email),
		ProjectID:    tokenData.ProjectID,
		ProviderID:   providerID,
		TierID:       tokenData.TierID,
		EndpointUsed: tokenData.EndpointUsed,
		Latency:      time.Since(start),
		Preview:      preview,
	}, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
