package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// Handlers implements the REST API endpoints.
type Handlers struct {
	personaService  *app.PersonaService
	chatService     *app.ChatService
	settingsStore   *config.SettingsStore
	providerManager *app.ProviderManager
	oauthFlow       *llm.GeminiOAuthFlow
	ctx             context.Context
}

func NewHandlers(
	personaService *app.PersonaService,
	chatService *app.ChatService,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	oauthFlow *llm.GeminiOAuthFlow,
	ctx context.Context,
) *Handlers {
	return &Handlers{
		personaService:  personaService,
		chatService:     chatService,
		settingsStore:   settingsStore,
		providerManager: providerManager,
		oauthFlow:       oauthFlow,
		ctx:             ctx,
	}
}

// --- Persona endpoints ---

// PersonaSummary is the API representation of a persona (no raw lore/template).
type PersonaSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	AnchorCount int      `json:"anchor_count"`
	AnchorTags  []string `json:"anchor_tags"`
}

// PersonaDetail is the full API representation of a persona.
type PersonaDetail struct {
	Meta           domain.PersonaMeta      `json:"meta"`
	SystemTemplate string                  `json:"system_template"`
	Lore           string                  `json:"lore"`
	Anchors        []domain.Anchor         `json:"anchors"`
	Generation     domain.GenerationConfig `json:"generation"`
	Variables      map[string]any          `json:"variables"`
}

func (h *Handlers) ListPersonas(w http.ResponseWriter, r *http.Request) {
	personas := h.personaService.GetAll()
	summaries := make([]PersonaSummary, 0, len(personas))

	for _, p := range personas {
		summaries = append(summaries, toSummary(p))
	}

	writeJSON(w, http.StatusOK, summaries)
}

func (h *Handlers) GetPersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	persona, ok := h.personaService.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "persona not found: "+id)
		return
	}

	// Return full detail using explicit DTO
	detail := PersonaDetail{
		Meta:           persona.Config.Meta,
		SystemTemplate: persona.Config.SystemTemplate,
		Lore:           persona.Lore,
		Anchors:        persona.Anchors,
		Generation:     persona.Config.Generation,
		Variables:      persona.Config.Variables,
	}

	writeJSON(w, http.StatusOK, detail)
}

func (h *Handlers) UpdatePersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.personaService.Get(id); !ok {
		writeError(w, http.StatusNotFound, "persona not found: "+id)
		return
	}

	var req PersonaDetail
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	updated := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           req.Meta,
			SystemTemplate: req.SystemTemplate,
			Generation:     req.Generation,
			Variables:      req.Variables,
		},
		Lore:    req.Lore,
		Anchors: req.Anchors,
	}

	if err := h.personaService.Update(id, updated); err != nil {
		writeError(w, http.StatusInternalServerError, "save persona: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *Handlers) ReloadPersonas(w http.ResponseWriter, r *http.Request) {
	if err := h.personaService.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "reloaded",
		"count":  formatInt(len(h.personaService.GetAll())),
	})
}

// --- Chat endpoint ---

type ChatRequest struct {
	ChannelID string `json:"channel_id"`
	PersonaID string `json:"persona_id"`
	Message   string `json:"message"`
}

type ChatResponseBody struct {
	Response string `json:"response"`
}

func (h *Handlers) Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	log.Printf("[CHAT] Received request - Channel: %s, Persona: %s, Message: %s",
		req.ChannelID, req.PersonaID, truncate(req.Message, 50))

	if req.Message == "" || req.PersonaID == "" {
		writeError(w, http.StatusBadRequest, "message and persona_id are required")
		return
	}

	if req.ChannelID == "" {
		req.ChannelID = "web-test"
	}

	response, err := h.chatService.HandleMessage(r.Context(), req.ChannelID, req.PersonaID, req.Message)
	if err != nil {
		log.Printf("[CHAT] Error: %v", err)
		// Distinguish between configuration errors and actual server errors
		if strings.Contains(err.Error(), "LLM provider not configured") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	log.Printf("[CHAT] Success - Response length: %d chars", len(response))
	writeJSON(w, http.StatusOK, ChatResponseBody{Response: response})
}

// --- Health ---

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Helpers ---

func toSummary(p *domain.PersonaModule) PersonaSummary {
	tagSet := make(map[string]bool)
	for _, a := range p.Anchors {
		for _, tag := range a.Tags {
			tagSet[tag] = true
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	return PersonaSummary{
		ID:          p.Config.Meta.ID,
		Name:        p.Config.Meta.Name,
		Description: p.Config.Meta.Description,
		Version:     p.Config.Meta.Version,
		Author:      p.Config.Meta.Author,
		AnchorCount: len(p.Anchors),
		AnchorTags:  tags,
	}
}

func formatInt(n int) string {
	return fmt.Sprintf("%d", n)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Settings endpoints ---

type SettingsResponse struct {
	GeminiAPIKey string `json:"gemini_api_key"`
	GeminiModel  string `json:"gemini_model"`
	DiscordToken string `json:"discord_token"`
	ProviderType string `json:"provider_type"`
}

type SettingsUpdateRequest struct {
	GeminiAPIKey string `json:"gemini_api_key"`
	GeminiModel  string `json:"gemini_model"`
	DiscordToken string `json:"discord_token"`
	ProviderType string `json:"provider_type"`
}

type ProviderStatusResponse struct {
	Configured   bool   `json:"configured"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, SettingsResponse{
		GeminiAPIKey: maskKey(settings.GeminiAPIKey),
		GeminiModel:  settings.GeminiModel,
		DiscordToken: maskKey(settings.DiscordToken),
		ProviderType: settings.ProviderType,
	})
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Load existing settings to preserve unchanged masked fields.
	existing, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	if req.GeminiAPIKey != "" && !isMasked(req.GeminiAPIKey) {
		existing.GeminiAPIKey = req.GeminiAPIKey
	}
	if req.GeminiModel != "" {
		existing.GeminiModel = req.GeminiModel
	}
	if req.DiscordToken != "" && !isMasked(req.DiscordToken) {
		existing.DiscordToken = req.DiscordToken
	}
	if req.ProviderType != "" {
		existing.ProviderType = req.ProviderType
	}

	log.Printf("[Settings] Saving settings - Model: %s, ProviderType: %s",
		existing.GeminiModel, existing.ProviderType)

	if err := h.settingsStore.Save(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "save settings: "+err.Error())
		return
	}

	// Hot-swap the LLM provider based on selected provider_type.
	if err := h.activateProvider(existing); err != nil {
		log.Printf("[Settings] Provider activation failed: %v", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("[Settings] Settings saved and provider activated successfully")
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// activateProvider creates and hot-swaps the LLM provider based on settings.
func (h *Handlers) activateProvider(s config.Settings) error {
	model := s.GeminiModel
	if model == "" {
		model = "gemini-2.5-pro"
	}

	log.Printf("[ActivateProvider] Type: %s, Model: %s", s.ProviderType, model)

	switch s.ProviderType {
	case config.ProviderOAuth:
		log.Printf("[ActivateProvider] Switching to OAuth provider")
		oauthData, err := h.oauthFlow.GetStatus()
		if err != nil || oauthData == nil {
			return fmt.Errorf("no OAuth credentials available")
		}
		provider, err := llm.NewGeminiOAuthProvider(h.ctx, oauthData, model)
		if err != nil {
			return fmt.Errorf("create OAuth provider: %w", err)
		}
		h.providerManager.Update(provider)
		log.Printf("provider hot-swapped: gemini-oauth (model: %s)", model)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return fmt.Errorf("invalid API key: %w", err)
		}
		h.providerManager.Update(provider)
		log.Printf("provider hot-swapped: gemini-api (model: %s)", model)

	default:
		// Auto mode: try API key first, then OAuth.
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}
			h.providerManager.Update(provider)
			log.Printf("provider hot-swapped: gemini-api (model: %s)", model)
		}
	}
	return nil
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:   h.providerManager.IsConfigured(),
		ProviderName: info.ProviderName,
		Model:        info.ProviderName, // ModelInfo doesn't expose model name separately
	})
}

// --- Models endpoint ---

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	var oauthToken *llm.OAuthTokenData
	if data, err := h.oauthFlow.GetStatus(); err == nil {
		oauthToken = data
	}

	models, err := llm.ListGeminiModels(r.Context(), settings.GeminiAPIKey, oauthToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models)
}

// maskKey shows only the last 4 characters of a secret, e.g. "***...abcd".
func maskKey(key string) string {
	if len(key) <= 4 {
		return key
	}
	return "***..." + key[len(key)-4:]
}

// isMasked checks if a value looks like a masked placeholder from GetSettings.
func isMasked(val string) bool {
	return len(val) > 3 && val[:3] == "***"
}

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
}

type OAuthStartResponse struct {
	AuthURL string `json:"auth_url"`
}

func (h *Handlers) OAuthStart(w http.ResponseWriter, r *http.Request) {
	var req OAuthStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
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

	authURL, err := h.oauthFlow.StartFlow(clientID, clientSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start OAuth flow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, OAuthStartResponse{AuthURL: authURL})
}

func (h *Handlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	tokenData, err := h.oauthFlow.HandleCallback(h.ctx, code, state)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<!doctype html><html><body><h2>OAuth Failed</h2><p>%s</p></body></html>`, err.Error())
		return
	}

	// Hot-swap the provider and save provider_type.
	model := "gemini-2.5-pro"
	if s, loadErr := h.settingsStore.Load(); loadErr == nil && s.GeminiModel != "" {
		model = s.GeminiModel
	}

	provider, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<!doctype html><html><body><h2>Provider Creation Failed</h2><p>%s</p></body></html>`, err.Error())
		return
	}
	h.providerManager.Update(provider)
	log.Printf("provider hot-swapped: gemini-oauth (project: %s)", tokenData.ProjectID)

	// Persist provider_type = oauth.
	if s, loadErr := h.settingsStore.Load(); loadErr == nil {
		s.ProviderType = config.ProviderOAuth
		_ = h.settingsStore.Save(s)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"/></head>`+
		`<body><h2>Gemini OAuth Complete</h2>`+
		`<p>You can close this window and return to KoukeBOT.</p>`+
		`<script>window.close()</script></body></html>`)
}

type OAuthStatusResponse struct {
	Authenticated bool   `json:"authenticated"`
	ProjectID     string `json:"project_id,omitempty"`
	Email         string `json:"email,omitempty"`
}

func (h *Handlers) OAuthStatus(w http.ResponseWriter, r *http.Request) {
	data, err := h.oauthFlow.GetStatus()
	if err != nil || data == nil {
		writeJSON(w, http.StatusOK, OAuthStatusResponse{Authenticated: false})
		return
	}
	writeJSON(w, http.StatusOK, OAuthStatusResponse{
		Authenticated: data.AccessToken != "",
		ProjectID:     data.ProjectID,
		Email:         data.Email,
	})
}
