package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (f *GeminiOAuthFlow) cleanupExpiredFlowsLocked() {
	now := time.Now()
	for id, flow := range f.flows {
		if flow.ExpiresAt.Before(now) {
			delete(f.flows, id)
		}
	}
}

func (f *GeminiOAuthFlow) resolveFlowLocked(flowID, state string) (*oauthFlowState, error) {
	if flowID != "" {
		flow, ok := f.flows[flowID]
		if !ok {
			return nil, errors.New("invalid or expired OAuth flow")
		}
		if state != "" && state != flow.State {
			return nil, errors.New("invalid OAuth state for flow")
		}
		delete(f.flows, flowID)
		return flow, nil
	}

	for id, flow := range f.flows {
		if flow.State == state {
			delete(f.flows, id)
			return flow, nil
		}
	}
	return nil, errors.New("invalid or expired OAuth state")
}

// StartFlow generates an OAuth authorization URL and stores in-flight state.
func (f *GeminiOAuthFlow) StartFlow(clientID, clientSecret, redirectURL string) (authURL string, err error) {
	info, err := f.StartFlowWithProvider(clientID, clientSecret, "local", redirectURL, ProviderGoogleGeminiCLI)
	if err != nil {
		return "", err
	}
	return info.AuthURL, nil
}

// StartFlowV2 creates a concurrent-safe OAuth flow and returns metadata for UI/API usage.
func (f *GeminiOAuthFlow) StartFlowV2(clientID, clientSecret, mode, redirectURL string) (*OAuthStartInfo, error) {
	return f.StartFlowWithProvider(clientID, clientSecret, mode, redirectURL, ProviderGoogleGeminiCLI)
}

// StartFlowWithProvider creates a concurrent-safe OAuth flow and returns metadata
// for UI/API usage with provider-specific OAuth scopes.
func (f *GeminiOAuthFlow) StartFlowWithProvider(clientID, clientSecret, mode, redirectURL, providerID string) (*OAuthStartInfo, error) {
	if strings.TrimSpace(clientID) == "" {
		return nil, errors.New("client_id is required")
	}
	if mode == "" {
		mode = "local"
	}
	spec := googleProviderSpec(providerID)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupExpiredFlowsLocked()

	redir := f.redirectURL
	if strings.TrimSpace(redir) == "" {
		redir = spec.RedirectURI
	}
	if strings.TrimSpace(redirectURL) != "" {
		redir = strings.TrimSpace(redirectURL)
	}

	flowID := generateFlowID()
	state := generateState()
	verifier, _ := generatePKCE()
	now := time.Now()
	flow := &oauthFlowState{
		FlowID:       flowID,
		State:        state,
		CodeVerifier: verifier,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redir,
		Mode:         mode,
		ProviderID:   spec.ID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(f.flowTTL),
	}
	f.flows[flowID] = flow

	return &OAuthStartInfo{
		FlowID:      flowID,
		AuthURL:     buildAuthURL(clientID, redir, state, verifier, spec.OAuthScopes),
		ExpiresAt:   flow.ExpiresAt,
		Mode:        mode,
		RedirectURL: redir,
		ProviderID:  spec.ID,
	}, nil
}

// HandleCallback processes the OAuth callback by state lookup.
func (f *GeminiOAuthFlow) HandleCallback(ctx context.Context, code, state string) (*OAuthTokenData, error) {
	return f.HandleCallbackWithFlow(ctx, "", code, state)
}

// HandleCallbackWithFlow processes the OAuth callback, exchanges code for tokens,
// discovers the project, and persists the result.
func (f *GeminiOAuthFlow) HandleCallbackWithFlow(ctx context.Context, flowID, code, state string) (*OAuthTokenData, error) {
	f.mu.Lock()
	f.cleanupExpiredFlowsLocked()
	flow, err := f.resolveFlowLocked(flowID, state)
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}

	token, err := exchangeCode(ctx, flow.ClientID, flow.ClientSecret, code, flow.CodeVerifier, flow.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	email, _ := fetchUserEmail(ctx, token.AccessToken)
	projectID, tierID, endpointUsed, err := discoverProjectWithProvider(
		ctx,
		token.AccessToken,
		flow.ProviderID,
		EndpointPolicyPrimaryThenSandbox,
	)
	if err != nil {
		logFlow.Warnf("project discovery failed during OAuth callback: %v (token will still be persisted)", err)
		if previous, loadErr := LoadStoredToken(f.tokenPath); loadErr == nil && previous != nil {
			if projectID == "" {
				projectID = strings.TrimSpace(previous.ProjectID)
			}
			if tierID == "" {
				tierID = strings.TrimSpace(previous.TierID)
			}
			if endpointUsed == "" {
				endpointUsed = strings.TrimSpace(previous.EndpointUsed)
			}
		}
		if endpointUsed == "" {
			spec := googleProviderSpec(flow.ProviderID)
			endpointUsed = firstEndpointOrDefault(resolveOAuthEndpointCandidates(spec.EndpointCandidates, EndpointPolicyPrimaryThenSandbox))
		}
		if tierID == "" {
			tierID = "unknown"
		}
	}

	data := &OAuthTokenData{
		ClientID:     flow.ClientID,
		ClientSecret: flow.ClientSecret,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		ProjectID:    projectID,
		Email:        email,
		ProviderID:   flow.ProviderID,
		TierID:       tierID,
		EndpointUsed: endpointUsed,
	}

	if err := saveTokenData(f.tokenPath, data); err != nil {
		logFlow.Warnf("failed to persist OAuth token: %v", err)
	}
	if f.poolPath != "" {
		if err := AddToOAuthPool(f.poolPath, data); err != nil {
			logFlow.Warnf("failed to update pool file: %v", err)
		}
	}

	return data, nil
}
