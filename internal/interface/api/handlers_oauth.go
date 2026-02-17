package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
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
	var loadedSettings config.Settings
	loadedSettings, _ = h.settingsStore.Load()
	endpointPolicy = llm.ResolveEndpointPolicy(loadedSettings.EndpointPolicy)
	settingProviderID, settingModel, _ := llm.ResolveProviderAndModel(loadedSettings.ProviderID, loadedSettings.ModelID, loadedSettings.GeminiModel)
	if settingProviderID == providerID && settingModel != "" {
		model = settingModel
	}

	provider, err := llm.NewGeminiOAuthProviderWithProvider(h.ctx, tokenData, providerID, model, endpointPolicy)
	if err != nil {
		return nil, fmt.Errorf("provider creation: %w", err)
	}

	pool := h.providerManager.GetPool()
	poolPath := h.oauthFlow.PoolPath()
	if pool == nil {
		pool = app.NewProviderPool()
		pool.SetPoolStorePath(poolPath)
		if loadedSettings.AutoRetryCount > 0 {
			pool.SetAutoRetryCount(loadedSettings.AutoRetryCount)
		}
		pool.SetTransientRecoveryWait(
			loadedSettings.TransientRecoveryWaitEnabled,
			time.Duration(loadedSettings.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
		h.providerManager.Update(pool)
	} else {
		pool.SetPoolStorePath(poolPath)
		if loadedSettings.AutoRetryCount > 0 {
			pool.SetAutoRetryCount(loadedSettings.AutoRetryCount)
		}
		pool.SetTransientRecoveryWait(
			loadedSettings.TransientRecoveryWaitEnabled,
			time.Duration(loadedSettings.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
	}
	pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	logAPI.Logf("OAuth account added to pool: %s (project: %s, pool size: %d)",
		tokenData.Email, tokenData.ProjectID, pool.Size())

	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		s.ProviderType = config.ProviderOAuth
		s.ProviderID = providerID
		if _, settingModelID := llm.SplitProviderModelID(s.ModelID); settingModelID == "" {
			s.ModelID = llm.DefaultProviderModelID(providerID)
		}
		_, s.GeminiModel, s.ModelID = llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
		_ = h.settingsStore.Save(s)
	}

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

	_, model := llm.SplitProviderModelID(llm.DefaultProviderModelID(providerID))
	endpointPolicy := llm.EndpointPolicyPrimaryThenSandbox
	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		endpointPolicy = llm.ResolveEndpointPolicy(s.EndpointPolicy)
		spid, smodel, _ := llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
		if spid == providerID && smodel != "" {
			model = smodel
		}
	}

	provider, err := llm.NewGeminiOAuthProviderWithProvider(h.ctx, tokenData, providerID, model, endpointPolicy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider creation: "+err.Error())
		return
	}

	pool := h.providerManager.GetPool()
	poolPath := h.oauthFlow.PoolPath()
	if pool == nil {
		pool = app.NewProviderPool()
		pool.SetPoolStorePath(poolPath)
		h.providerManager.Update(pool)
	} else {
		pool.SetPoolStorePath(poolPath)
	}
	pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	logAPI.Logf("Imported pasted token for %s (project: %s, pool size: %d)",
		tokenData.Email, tokenData.ProjectID, pool.Size())

	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		s.ProviderType = config.ProviderOAuth
		s.ProviderID = providerID
		if _, mid := llm.SplitProviderModelID(s.ModelID); mid == "" {
			s.ModelID = llm.DefaultProviderModelID(providerID)
		}
		_, s.GeminiModel, s.ModelID = llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
		_ = h.settingsStore.Save(s)
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

	_, model := llm.SplitProviderModelID(llm.DefaultProviderModelID(providerID))
	endpointPolicy := llm.EndpointPolicyPrimaryThenSandbox
	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		endpointPolicy = llm.ResolveEndpointPolicy(s.EndpointPolicy)
		spid, smodel, _ := llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
		if spid == providerID && smodel != "" {
			model = smodel
		}
	}

	provider, err := llm.NewGeminiOAuthProviderWithProvider(h.ctx, tokenData, providerID, model, endpointPolicy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider creation: "+err.Error())
		return
	}

	pool := h.providerManager.GetPool()
	poolPath := h.oauthFlow.PoolPath()
	if pool == nil {
		pool = app.NewProviderPool()
		pool.SetPoolStorePath(poolPath)
		h.providerManager.Update(pool)
	} else {
		pool.SetPoolStorePath(poolPath)
	}
	pool.AddOrReplaceWithUsage(provider, tokenData.Email, tokenData.UsageStats)
	logAPI.Logf("Imported Gemini CLI token for %s (project: %s, pool size: %d)",
		tokenData.Email, tokenData.ProjectID, pool.Size())

	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		s.ProviderType = config.ProviderOAuth
		s.ProviderID = providerID
		if _, mid := llm.SplitProviderModelID(s.ModelID); mid == "" {
			s.ModelID = llm.DefaultProviderModelID(providerID)
		}
		_, s.GeminiModel, s.ModelID = llm.ResolveProviderAndModel(s.ProviderID, s.ModelID, s.GeminiModel)
		_ = h.settingsStore.Save(s)
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
