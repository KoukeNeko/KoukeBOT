package llm

// gemini_oauth_provider.go — GeminiOAuthProvider struct, constructors, and
// the non-streaming Provider interface methods (ModelInfo, RegisterTools, etc.).
//
// Separation rationale: the struct lifecycle and capability metadata are stable;
// streaming logic lives in gemini_oauth_stream.go, HTTP transport in
// gemini_oauth_http.go, and CA API types + schema cleaning in gemini_oauth_schema.go.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"golang.org/x/oauth2"
)

var logOAuth = logger.New("llm", logger.Yellow)

// GeminiOAuthProvider uses OAuth token + Cloud Code Assist API to access Gemini.
// The genai SDK requires an API key for BackendGeminiAPI, so we bypass it
// and make raw HTTP calls to cloudcode-pa.googleapis.com with Bearer auth.
type GeminiOAuthProvider struct {
	httpClient         *http.Client
	providerID         string
	model              string
	projectID          string
	endpointPolicy     string
	endpointCandidates []string
	endpointMu         sync.RWMutex
	effectiveEndpoint  string
	tools              []domain.Tool // Registered tools for function calling
}

// NewGeminiOAuthProvider creates a provider using OAuth credentials via Cloud Code Assist API.
func NewGeminiOAuthProvider(ctx context.Context, tokenData *OAuthTokenData, model string) (*GeminiOAuthProvider, error) {
	return NewGeminiOAuthProviderWithProvider(
		ctx,
		tokenData,
		ProviderGoogleGeminiCLI,
		model,
		EndpointPolicyPrimaryThenSandbox,
	)
}

// NewGeminiOAuthProviderWithProvider creates a provider with explicit provider identity
// and endpoint policy/candidate configuration.
func NewGeminiOAuthProviderWithProvider(
	ctx context.Context,
	tokenData *OAuthTokenData,
	providerID string,
	model string,
	endpointPolicy string,
) (*GeminiOAuthProvider, error) {
	tokenSource := NewTokenSource(ctx, tokenData)
	httpClient := oauth2.NewClient(ctx, tokenSource)
	pid := NormalizeGoogleProviderID(providerID)
	spec := googleProviderSpec(pid)
	candidates := resolveOAuthEndpointCandidates(spec.EndpointCandidates, endpointPolicy)
	normalizedModel := NormalizeGoogleModelAlias(model)
	if normalizedModel == "" {
		normalizedModel = spec.DefaultModel
	}

	return &GeminiOAuthProvider{
		httpClient:         httpClient,
		providerID:         pid,
		model:              normalizedModel,
		projectID:          tokenData.ProjectID,
		endpointPolicy:     ResolveEndpointPolicy(endpointPolicy),
		endpointCandidates: candidates,
		effectiveEndpoint:  candidates[0],
	}, nil
}

// WithModel returns a shallow clone of this provider configured for a different model.
// The HTTP client (and thus OAuth credentials) is shared.
func (g *GeminiOAuthProvider) WithModel(model string) Provider {
	g.endpointMu.RLock()
	effective := g.effectiveEndpoint
	candidates := append([]string(nil), g.endpointCandidates...)
	g.endpointMu.RUnlock()

	return &GeminiOAuthProvider{
		httpClient:         g.httpClient,
		providerID:         g.providerID,
		model:              NormalizeGoogleModelAlias(model),
		projectID:          g.projectID,
		endpointPolicy:     g.endpointPolicy,
		endpointCandidates: candidates,
		effectiveEndpoint:  effective,
		tools:              append([]domain.Tool(nil), g.tools...),
	}
}

// ModelInfo returns the capabilities of the currently configured model.
func (g *GeminiOAuthProvider) ModelInfo() ModelCapabilities {
	supportsThinking := strings.Contains(g.model, "2.5") ||
		strings.Contains(g.model, "3") ||
		strings.Contains(g.model, "thinking")

	return ModelCapabilities{
		SupportsThinking:  supportsThinking,
		ContextWindow:     1_000_000,
		MaxOutputTokens:   resolveMaxTokensForModel(g.model, defaultMaxOutputTokens),
		SupportsStreaming: true,
		ProviderName:      fmt.Sprintf("gemini-oauth/%s", g.providerID),
		Model:             g.model,
	}
}

// EffectiveEndpoint returns the endpoint of the last successful OAuth request.
func (g *GeminiOAuthProvider) EffectiveEndpoint() string {
	g.endpointMu.RLock()
	defer g.endpointMu.RUnlock()
	return g.effectiveEndpoint
}

// RegisterTools registers tools for function calling (OAuth provider).
// Tools will be sent as functionDeclarations in every API request.
func (g *GeminiOAuthProvider) RegisterTools(tools []domain.Tool) {
	g.tools = tools
}
