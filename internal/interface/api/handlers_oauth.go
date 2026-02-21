package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// --- OAuth endpoints ---

type OAuthDetectResponse struct {
	Found        bool   `json:"found"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (h *Handlers) OAuthDetect(w http.ResponseWriter, r *http.Request) {
	creds, err := llm.DetectGeminiCLI()
	if err != nil {
		writeJSON(w, http.StatusOK, OAuthDetectResponse{Found: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, OAuthDetectResponse{
		Found:        true,
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
	})
}

type OAuthStartRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ProviderID   string `json:"provider_id"`
	Mode         string `json:"mode"`         // "local" | "remote"
	RedirectURL  string `json:"redirect_url"` // optional override
}

type OAuthStartResponse struct {
	FlowID     string `json:"flow_id"`
	AuthURL    string `json:"auth_url"`
	ExpiresAt  string `json:"expires_at"`
	Mode       string `json:"mode"`
	ProviderID string `json:"provider_id,omitempty"`
}

func (h *Handlers) OAuthStart(w http.ResponseWriter, r *http.Request) {
	var req OAuthStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	providerID := llm.NormalizeGoogleProviderID(req.ProviderID)
	if providerID == "" {
		providerID = llm.ProviderGoogleAntigravity
	}

	// If no credentials provided, try auto-detect.
	clientID := req.ClientID
	clientSecret := req.ClientSecret
	if clientID == "" {
		creds, err := llm.DetectGeminiCLI()
		if err != nil {
			writeError(w, http.StatusBadRequest, "Gemini CLI not found and no credentials provided: "+err.Error())
			return
		}
		clientID = creds.ClientID
		clientSecret = creds.ClientSecret
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		if isLikelyLocalHost(r.Host) {
			mode = "local"
		} else {
			mode = "remote"
		}
	}
	if mode != "local" && mode != "remote" {
		writeError(w, http.StatusBadRequest, "mode must be 'local' or 'remote'")
		return
	}

	redirectURL := strings.TrimSpace(req.RedirectURL)
	if redirectURL == "" && mode == "remote" {
		base := derivePublicBaseURL(r)
		if base == "" {
			writeError(w, http.StatusBadRequest, "cannot derive public callback URL for remote mode")
			return
		}
		redirectURL = base + "/api/oauth/gemini/callback"
	}

	info, err := h.oauthFlow.StartFlowWithProvider(clientID, clientSecret, mode, redirectURL, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start OAuth flow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, OAuthStartResponse{
		FlowID:     info.FlowID,
		AuthURL:    info.AuthURL,
		ExpiresAt:  info.ExpiresAt.Format(time.RFC3339),
		Mode:       info.Mode,
		ProviderID: info.ProviderID,
	})
}

// completeOAuthCallback is the shared logic for both automatic and manual OAuth callbacks.
func (h *Handlers) completeOAuthCallback(flowID, code, state, expectedProviderID string) (*llm.OAuthTokenData, error) {
	tokenData, err := h.oauthFlow.HandleCallbackWithFlow(h.ctx, flowID, code, state)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	providerID := llm.NormalizeGoogleProviderID(tokenData.ProviderID)
	if providerID == "" {
		providerID = llm.ProviderGoogleAntigravity
	}
	if expected := llm.NormalizeGoogleProviderID(expectedProviderID); expected != "" && expected != providerID {
		return nil, fmt.Errorf("OAuth provider mismatch: expected %s, got %s", expected, providerID)
	}
	tokenData.ProviderID = providerID

	_, model := llm.SplitProviderModelID(llm.DefaultProviderModelID(providerID))
	if model == "" {
		model = "gemini-3-pro-preview"
	}
	endpointPolicy := llm.EndpointPolicyPrimaryThenSandbox
	loadedSettings := h.loadSettingsOrDefault()
	endpointPolicy = llm.ResolveEndpointPolicy(loadedSettings.EndpointPolicy)
	settingProviderID, settingModel, _ := llm.ResolveProviderAndModel(loadedSettings.ProviderID, loadedSettings.ModelID, loadedSettings.GeminiModel)
	if settingProviderID == providerID && settingModel != "" {
		model = settingModel
	}

	provider, err := llm.NewGeminiOAuthProviderWithProvider(h.ctx, tokenData, providerID, model, endpointPolicy)
	if err != nil {
		return nil, fmt.Errorf("provider creation: %w", err)
	}

	pool := h.ensureOAuthPool(loadedSettings, providerID, model)
	pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	logAPI.Logf("OAuth account added to pool: %s (project: %s, pool size: %d)",
		tokenData.Email, tokenData.ProjectID, pool.Size())

	h.persistOAuthProviderSelection(providerID)

	return tokenData, nil
}

// OAuthCallback handles the automatic redirect from Google (GET).
func (h *Handlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	flowID := r.URL.Query().Get("flow_id")

	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	tokenData, err := h.completeOAuthCallback(flowID, code, state, "")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<!doctype html><html><body><h2>OAuth Failed</h2><p>%s</p></body></html>`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"/></head>`+
		`<body><h2>Gemini OAuth Complete</h2>`+
		`<p>Account <b>%s</b> added to pool. You can close this window and return to KoukeBOT.</p>`+
		`<script>window.close()</script></body></html>`, tokenData.Email)
}

// OAuthCallbackManual handles the paste-URL fallback for remote deployments (POST).
// The user pastes the localhost callback URL from the browser address bar.
func (h *Handlers) OAuthCallbackManual(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallbackURL string `json:"callback_url"`
		FlowID      string `json:"flow_id"`
		ProviderID  string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	parsed, err := url.Parse(req.CallbackURL)
	if err != nil || parsed.Query().Get("code") == "" {
		writeError(w, http.StatusBadRequest, "invalid callback URL: missing code parameter")
		return
	}

	code := parsed.Query().Get("code")
	state := parsed.Query().Get("state")

	tokenData, err := h.completeOAuthCallback(req.FlowID, code, state, req.ProviderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":         tokenData.Email,
		"project_id":    tokenData.ProjectID,
		"tier_id":       tokenData.TierID,
		"endpoint_used": tokenData.EndpointUsed,
		"provider_id":   tokenData.ProviderID,
		"status":        "ok",
	})
}

// OAuthComplete handles explicit flow completion for remote fallback.
func (h *Handlers) OAuthComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FlowID      string `json:"flow_id"`
		CallbackURL string `json:"callback_url"`
		ProviderID  string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.FlowID) == "" {
		writeError(w, http.StatusBadRequest, "flow_id is required")
		return
	}

	parsed, err := url.Parse(req.CallbackURL)
	if err != nil || parsed.Query().Get("code") == "" {
		writeError(w, http.StatusBadRequest, "invalid callback URL: missing code parameter")
		return
	}

	code := parsed.Query().Get("code")
	state := parsed.Query().Get("state")

	tokenData, err := h.completeOAuthCallback(req.FlowID, code, state, req.ProviderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":         tokenData.Email,
		"project_id":    tokenData.ProjectID,
		"tier_id":       tokenData.TierID,
		"endpoint_used": tokenData.EndpointUsed,
		"provider_id":   tokenData.ProviderID,
		"status":        "ok",
	})
}

func isLikelyLocalHost(hostport string) bool {
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.TrimSpace(strings.ToLower(host)), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func derivePublicBaseURL(r *http.Request) string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimSuffix(v, "/")
	}

	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}

	return strings.TrimSuffix(fmt.Sprintf("%s://%s", proto, host), "/")
}


// finalizeTokenImport resolves the model from settings, creates the OAuth
// provider, adds it to the pool, and persists the provider selection.
// Both OAuthImportPastedToken and OAuthImportCLIToken call this after
// their unique import logic resolves providerID and sets tokenData.ProviderID.
func (h *Handlers) finalizeTokenImport(tokenData *llm.OAuthTokenData, providerID string) error {
	_, model := llm.SplitProviderModelID(llm.DefaultProviderModelID(providerID))
	loadedSettings := h.loadSettingsOrDefault()
	endpointPolicy := llm.ResolveEndpointPolicy(loadedSettings.EndpointPolicy)
	if loadedSettings.ProviderID != "" {
		settingProviderID, settingModel, _ := llm.ResolveProviderAndModel(loadedSettings.ProviderID, loadedSettings.ModelID, loadedSettings.GeminiModel)
		if settingProviderID == providerID && settingModel != "" {
			model = settingModel
		}
	}

	provider, err := llm.NewGeminiOAuthProviderWithProvider(h.ctx, tokenData, providerID, model, endpointPolicy)
	if err != nil {
		return fmt.Errorf("provider creation: %w", err)
	}

	pool := h.ensureOAuthPool(loadedSettings, providerID, model)
	pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)

	h.persistOAuthProviderSelection(providerID)
	return nil
}

// OAuthImportPastedToken accepts raw oauth_creds.json content pasted by the user.
// This supports PaaS deployments where SSH is not available — user runs `gemini` locally
// and pastes the token JSON.
func (h *Handlers) OAuthImportPastedToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TokenJSON    string `json:"token_json"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		ProviderID   string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.TokenJSON == "" {
		writeError(w, http.StatusBadRequest, "token_json is required")
		return
	}

	tokenData, err := h.oauthFlow.ImportTokenFromJSON(h.ctx, []byte(req.TokenJSON), req.ClientID, req.ClientSecret)
	if err != nil {
		writeError(w, http.StatusBadRequest, "import failed: "+err.Error())
		return
	}
	providerID := llm.NormalizeGoogleProviderID(req.ProviderID)
	if providerID == "" {
		providerID = llm.NormalizeGoogleProviderID(tokenData.ProviderID)
	}
	if providerID == "" {
		providerID = llm.ProviderGoogleAntigravity
	}
	tokenData.ProviderID = providerID

	if err := h.finalizeTokenImport(tokenData, providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logAPI.Logf("Imported pasted token for %s (project: %s)", tokenData.Email, tokenData.ProjectID)

	writeJSON(w, http.StatusOK, map[string]string{
		"email":         tokenData.Email,
		"project_id":    tokenData.ProjectID,
		"tier_id":       tokenData.TierID,
		"endpoint_used": tokenData.EndpointUsed,
		"provider_id":   tokenData.ProviderID,
		"status":        "ok",
	})
}

// OAuthImportCLIToken reads the stored Gemini CLI token from the server filesystem
// and imports it into the pool. The user must have run `gemini` CLI on the server first.
func (h *Handlers) OAuthImportCLIToken(w http.ResponseWriter, r *http.Request) {
	tokenData, err := h.oauthFlow.ImportGeminiCLIToken(h.ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "import failed: "+err.Error())
		return
	}
	providerID := llm.NormalizeGoogleProviderID(r.URL.Query().Get("provider_id"))
	if providerID == "" {
		providerID = llm.NormalizeGoogleProviderID(tokenData.ProviderID)
	}
	if providerID == "" {
		providerID = llm.ProviderGoogleAntigravity
	}
	tokenData.ProviderID = providerID

	if err := h.finalizeTokenImport(tokenData, providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logAPI.Logf("Imported Gemini CLI token for %s (project: %s)", tokenData.Email, tokenData.ProjectID)

	writeJSON(w, http.StatusOK, map[string]string{
		"email":         tokenData.Email,
		"project_id":    tokenData.ProjectID,
		"tier_id":       tokenData.TierID,
		"endpoint_used": tokenData.EndpointUsed,
		"provider_id":   tokenData.ProviderID,
		"status":        "ok",
	})
}

type OAuthStatusResponse struct {
	Authenticated bool             `json:"authenticated"`
	ProjectID     string           `json:"project_id,omitempty"`
	Email         string           `json:"email,omitempty"`
	ProviderID    string           `json:"provider_id,omitempty"`
	TierID        string           `json:"tier_id,omitempty"`
	EndpointUsed  string           `json:"endpoint_used,omitempty"`
	Pool          []app.PoolStatus `json:"pool,omitempty"`
	PoolSize      int              `json:"pool_size"`
}

func (h *Handlers) OAuthStatus(w http.ResponseWriter, r *http.Request) {
	resp := OAuthStatusResponse{}

	// Check pool status
	if pool := h.providerManager.GetPool(); pool != nil {
		entries := pool.Entries()
		resp.Pool = entries
		resp.PoolSize = len(entries)
		resp.Authenticated = len(entries) > 0
		if len(entries) > 0 {
			resp.Email = entries[0].Email
		}
	} else {
		// Fall back to single token status
		data, err := h.oauthFlow.GetStatus()
		if err == nil && data != nil {
			resp.Authenticated = data.AccessToken != ""
			resp.ProjectID = data.ProjectID
			resp.Email = data.Email
			resp.ProviderID = data.ProviderID
			resp.TierID = data.TierID
			resp.EndpointUsed = data.EndpointUsed
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// OAuthPoolList returns the status of all accounts in the OAuth pool.
func (h *Handlers) OAuthPoolList(w http.ResponseWriter, r *http.Request) {
	pool := h.providerManager.GetPool()
	if pool == nil {
		writeJSON(w, http.StatusOK, []app.PoolStatus{})
		return
	}
	writeJSON(w, http.StatusOK, pool.Entries())
}

type OAuthPoolVerifyRequest struct {
	Email          string `json:"email,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type OAuthPoolVerifyResult struct {
	Email        string `json:"email"`
	ProjectID    string `json:"project_id,omitempty"`
	ProviderID   string `json:"provider_id,omitempty"`
	TierID       string `json:"tier_id,omitempty"`
	EndpointUsed string `json:"endpoint_used,omitempty"`
	Verified     bool   `json:"verified"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
	Preview      string `json:"preview,omitempty"`
	Error        string `json:"error,omitempty"`
	CheckedAt    string `json:"checked_at"`
}

type OAuthPoolVerifyResponse struct {
	Status        string                  `json:"status"`
	Model         string                  `json:"model"`
	Endpoint      string                  `json:"endpoint"`
	CheckedAt     string                  `json:"checked_at"`
	Results       []OAuthPoolVerifyResult `json:"results"`
	VerifiedCount int                     `json:"verified_count"`
	FailedCount   int                     `json:"failed_count"`
	PoolSize      int                     `json:"pool_size"`
}

// OAuthPoolVerify probes OAuth accounts end-to-end:
// refresh token -> discover project -> send a minimal generation request.
func (h *Handlers) OAuthPoolVerify(w http.ResponseWriter, r *http.Request) {
	var req OAuthPoolVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	settings := h.loadSettingsOrDefault()
	settingsProviderID, model, _ := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
	providerID := settingsProviderID
	if override := llm.NormalizeGoogleProviderID(req.ProviderID); override != "" {
		providerID = override
		if llm.NormalizeGoogleProviderID(settingsProviderID) != override {
			if _, defaultModel := llm.SplitProviderModelID(llm.DefaultProviderModelID(override)); defaultModel != "" {
				model = defaultModel
			}
		}
	}
	providerID = llm.NormalizeGoogleProviderID(providerID)
	if providerID == "" {
		providerID = llm.ProviderGoogleAntigravity
	}
	if strings.TrimSpace(model) == "" {
		if _, defaultModel := llm.SplitProviderModelID(llm.DefaultProviderModelID(providerID)); defaultModel != "" {
			model = defaultModel
		}
	}
	endpointPolicy := llm.ResolveEndpointPolicy(settings.EndpointPolicy)

	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 25
	}
	if timeoutSeconds > 120 {
		timeoutSeconds = 120
	}
	verifyTimeout := time.Duration(timeoutSeconds) * time.Second
	targetEmail := strings.TrimSpace(req.Email)

	poolPath := h.oauthFlow.PoolPath()
	tokens, err := llm.LoadOAuthPool(poolPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load OAuth pool: "+err.Error())
		return
	}
	if len(tokens) == 0 {
		if token, statusErr := h.oauthFlow.GetStatus(); statusErr == nil && token != nil {
			tokens = []*llm.OAuthTokenData{token}
		}
	}
	if targetEmail != "" {
		filtered := make([]*llm.OAuthTokenData, 0, len(tokens))
		for _, token := range tokens {
			if token == nil {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(token.Email), targetEmail) {
				filtered = append(filtered, token)
			}
		}
		tokens = filtered
	}
	if len(tokens) == 0 {
		writeError(w, http.StatusNotFound, "no OAuth account found to verify")
		return
	}

	pool := h.ensureOAuthPool(settings, providerID, model)
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]OAuthPoolVerifyResult, 0, len(tokens))
	verifiedCount := 0

	for _, tokenData := range tokens {
		if tokenData == nil {
			continue
		}
		originalEmail := strings.TrimSpace(tokenData.Email)
		result := OAuthPoolVerifyResult{
			Email:        firstNonEmpty(targetEmail, originalEmail),
			ProviderID:   llm.NormalizeGoogleProviderID(firstNonEmpty(tokenData.ProviderID, providerID)),
			ProjectID:    strings.TrimSpace(tokenData.ProjectID),
			TierID:       strings.TrimSpace(tokenData.TierID),
			EndpointUsed: strings.TrimSpace(tokenData.EndpointUsed),
			CheckedAt:    checkedAt,
		}

		verifyCtx, cancel := context.WithTimeout(r.Context(), verifyTimeout)
		probe, verifyErr := llm.VerifyOAuthAccountWithProvider(
			verifyCtx,
			tokenData,
			result.ProviderID,
			model,
			endpointPolicy,
		)
		cancel()
		if verifyErr != nil {
			result.Verified = false
			result.Error = verifyErr.Error()
			emailForPool := firstNonEmpty(strings.TrimSpace(tokenData.Email), originalEmail, result.Email)
			if emailForPool != "" {
				result.Email = emailForPool
				pool.MarkTransientFailure(emailForPool, verifyErr, 45*time.Second)
			}
			results = append(results, result)
			continue
		}

		if probe != nil {
			result.Email = firstNonEmpty(probe.Email, tokenData.Email, originalEmail, result.Email)
			result.ProviderID = llm.NormalizeGoogleProviderID(firstNonEmpty(probe.ProviderID, tokenData.ProviderID, result.ProviderID))
			result.ProjectID = firstNonEmpty(probe.ProjectID, tokenData.ProjectID, result.ProjectID)
			result.TierID = firstNonEmpty(probe.TierID, tokenData.TierID, result.TierID)
			result.EndpointUsed = firstNonEmpty(probe.EndpointUsed, tokenData.EndpointUsed, result.EndpointUsed)
			result.Preview = strings.TrimSpace(probe.Preview)
			result.LatencyMS = probe.Latency.Milliseconds()
		}

		provider, providerErr := llm.NewGeminiOAuthProviderWithProvider(
			h.ctx,
			tokenData,
			result.ProviderID,
			model,
			endpointPolicy,
		)
		if providerErr != nil {
			result.Verified = false
			result.Error = "provider creation: " + providerErr.Error()
			if result.Email != "" {
				pool.MarkTransientFailure(result.Email, providerErr, 45*time.Second)
			}
			results = append(results, result)
			continue
		}

		emailForPool := firstNonEmpty(result.Email, tokenData.Email, originalEmail, targetEmail)
		if emailForPool != "" {
			result.Email = emailForPool
			pool.AddOrReplaceWithUsage(provider, emailForPool, tokenData.UsageStats)
		}
		result.Verified = true
		verifiedCount++
		results = append(results, result)
	}

	if poolPath != "" {
		if err := llm.SaveOAuthPool(poolPath, tokens); err != nil {
			logAPI.Warnf("OAuth pool verify: persist pool failed: %v", err)
		}
	}

	failedCount := len(results) - verifiedCount
	logAPI.Logf("OAuth pool verify: checked=%d ok=%d fail=%d provider=%s model=%s", len(results), verifiedCount, failedCount, providerID, model)

	writeJSON(w, http.StatusOK, OAuthPoolVerifyResponse{
		Status:        "ok",
		Model:         model,
		Endpoint:      "cloudcode-pa.googleapis.com/*:streamGenerateContent?alt=sse",
		CheckedAt:     checkedAt,
		Results:       results,
		VerifiedCount: verifiedCount,
		FailedCount:   failedCount,
		PoolSize:      pool.Size(),
	})
}

// OAuthPoolRemove removes an account from the OAuth pool by email.
func (h *Handlers) OAuthPoolRemove(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	pool := h.providerManager.GetPool()
	if pool == nil {
		writeError(w, http.StatusNotFound, "no OAuth pool active")
		return
	}

	if !pool.Remove(email) {
		writeError(w, http.StatusNotFound, "account not found in pool: "+email)
		return
	}

	// Also remove from pool storage file
	poolPath := h.oauthFlow.PoolPath()
	if poolPath != "" {
		if err := llm.RemoveFromOAuthPool(poolPath, email); err != nil {
			logAPI.Warnf("failed to remove %s from pool file: %v", email, err)
		}
	}

	logAPI.Logf("OAuth account removed from pool: %s (remaining: %d)", email, pool.Size())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "removed",
		"email":     email,
		"pool_size": pool.Size(),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
