package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

const aiStudioVerifyTimeout = 12 * time.Second

type AIStudioSessionStatusResponse struct {
	Configured bool                          `json:"configured"`
	Session    *AIStudioSessionMaskedPayload `json:"session,omitempty"`
}

type AIStudioSessionMaskedPayload struct {
	Endpoint           string `json:"endpoint,omitempty"`
	APIKey             string `json:"api_key,omitempty"`
	XBrowserValidation string `json:"x_browser_validation,omitempty"`
	XClientData        string `json:"x_client_data,omitempty"`
	XGoogAuthUser      string `json:"x_goog_authuser,omitempty"`
	XGoogExt519733851  string `json:"x_goog_ext_519733851_bin,omitempty"`
	Cookie             string `json:"cookie,omitempty"`
	Authorization      string `json:"authorization,omitempty"`
	UserAgent          string `json:"user_agent,omitempty"`
	AcceptLanguage     string `json:"accept_language,omitempty"`
	Origin             string `json:"origin,omitempty"`
	Referer            string `json:"referer,omitempty"`
	XBrowserChannel    string `json:"x_browser_channel,omitempty"`
	XBrowserCopyright  string `json:"x_browser_copyright,omitempty"`
	XBrowserYear       string `json:"x_browser_year,omitempty"`
}

type AIStudioSessionVerifyRequest struct {
	Session *llm.AIStudioSession `json:"session,omitempty"`
	Model   string               `json:"model,omitempty"`
}

func (h *Handlers) AIStudioSessionGet(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.aiStudioSessionPath)
	if path == "" {
		writeError(w, http.StatusNotImplemented, "AI Studio session path is not configured")
		return
	}

	session, err := llm.LoadAIStudioSession(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load AI Studio session: "+err.Error())
		return
	}
	if session == nil {
		writeJSON(w, http.StatusOK, AIStudioSessionStatusResponse{Configured: false})
		return
	}

	writeJSON(w, http.StatusOK, AIStudioSessionStatusResponse{
		Configured: true,
		Session:    maskAIStudioSession(session),
	})
}

func (h *Handlers) AIStudioSessionPut(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.aiStudioSessionPath)
	if path == "" {
		writeError(w, http.StatusNotImplemented, "AI Studio session path is not configured")
		return
	}

	var req llm.AIStudioSession
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: multiple top-level objects are not allowed")
		return
	}

	if err := llm.SaveAIStudioSession(path, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	model := h.resolveActiveModelID()
	provider, err := llm.NewAIStudioRPCProvider(&req, model)
	if err != nil {
		// Persisted session is valid JSON but provider construction failed.
		writeError(w, http.StatusBadRequest, "invalid AI Studio session: "+err.Error())
		return
	}
	h.updateAIStudioFallbackProvider(provider)
	logAPI.Logf(
		"AI Studio session saved (api_key=%s cookie=%s authorization=%s)",
		summarizeSecret(req.APIKey),
		summarizeSecret(req.Cookie),
		summarizeSecret(req.Authorization),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "saved",
		"configured": true,
	})
}

func (h *Handlers) AIStudioSessionDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.aiStudioSessionPath)
	if path == "" {
		writeError(w, http.StatusNotImplemented, "AI Studio session path is not configured")
		return
	}

	if err := llm.DeleteAIStudioSession(path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.updateAIStudioFallbackProvider(nil)
	logAPI.Logf("AI Studio session deleted")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) AIStudioSessionVerify(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.aiStudioSessionPath)
	if path == "" {
		writeError(w, http.StatusNotImplemented, "AI Studio session path is not configured")
		return
	}

	var req AIStudioSessionVerifyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: multiple top-level objects are not allowed")
		return
	}

	session := req.Session
	if session == nil {
		loaded, err := llm.LoadAIStudioSession(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load AI Studio session: "+err.Error())
			return
		}
		if loaded == nil {
			writeError(w, http.StatusNotFound, "AI Studio session is not configured")
			return
		}
		session = loaded
	}

	model := llm.NormalizeGoogleModelAlias(strings.TrimSpace(req.Model))
	if model == "" {
		model = h.resolveActiveModelID()
	}

	verifyCtx, cancel := context.WithTimeout(r.Context(), aiStudioVerifyTimeout)
	defer cancel()
	if err := llm.VerifyAIStudioSession(verifyCtx, session, model); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"verified": false,
			"model":    model,
			"error":    err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"verified": true,
		"model":    model,
	})
}

func (h *Handlers) resolveActiveModelID() string {
	if h.settingsStore != nil {
		if settings, err := h.settingsStore.Load(); err == nil {
			_, model, _ := llm.ResolveProviderAndModel(settings.ProviderID, settings.ModelID, settings.GeminiModel)
			if normalized := llm.NormalizeGoogleModelAlias(model); normalized != "" {
				return normalized
			}
		}
	}
	if h.providerManager != nil {
		if model := llm.NormalizeGoogleModelAlias(strings.TrimSpace(h.providerManager.ModelInfo().Model)); model != "" {
			return model
		}
	}
	return "gemini-2.5-pro"
}

func (h *Handlers) updateAIStudioFallbackProvider(provider llm.Provider) {
	if h == nil || h.providerManager == nil {
		return
	}
	pool := h.providerManager.GetPool()
	if pool == nil {
		if !h.usingAIStudioDirectProvider() {
			return
		}
		h.providerManager.Update(provider)
		return
	}
	pool.SetOAuthExhaustionFallback(provider)
}

func (h *Handlers) usingAIStudioDirectProvider() bool {
	if h == nil || h.settingsStore == nil {
		return false
	}
	settings, err := h.settingsStore.Load()
	if err != nil {
		return false
	}
	return llm.NormalizeGoogleProviderID(settings.ProviderID) == llm.ProviderAIStudioRPC
}

func maskAIStudioSession(session *llm.AIStudioSession) *AIStudioSessionMaskedPayload {
	if session == nil {
		return nil
	}
	return &AIStudioSessionMaskedPayload{
		Endpoint:           strings.TrimSpace(session.Endpoint),
		APIKey:             summarizeSecret(session.APIKey),
		XBrowserValidation: summarizeSecret(session.XBrowserValidation),
		XClientData:        summarizeSecret(session.XClientData),
		XGoogAuthUser:      summarizeSecret(session.XGoogAuthUser),
		XGoogExt519733851:  summarizeSecret(session.XGoogExt519733851),
		Cookie:             summarizeSecret(session.Cookie),
		Authorization:      summarizeSecret(session.Authorization),
		UserAgent:          summarizeSecret(session.UserAgent),
		AcceptLanguage:     strings.TrimSpace(session.AcceptLanguage),
		Origin:             strings.TrimSpace(session.Origin),
		Referer:            strings.TrimSpace(session.Referer),
		XBrowserChannel:    strings.TrimSpace(session.XBrowserChannel),
		XBrowserCopyright:  strings.TrimSpace(session.XBrowserCopyright),
		XBrowserYear:       strings.TrimSpace(session.XBrowserYear),
	}
}

func summarizeSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("set(len=%d)", len(trimmed))
}
