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
	ctx             context.Context
}

func NewHandlers(
	personaService *app.PersonaService,
	chatService *app.ChatService,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	ctx context.Context,
) *Handlers {
	return &Handlers{
		personaService:  personaService,
		chatService:     chatService,
		settingsStore:   settingsStore,
		providerManager: providerManager,
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
	Meta       domain.PersonaMeta      `json:"meta"`
	Lore       string                  `json:"lore"`
	Anchors    []domain.Anchor         `json:"anchors"`
	Generation domain.GenerationConfig `json:"generation"`
	Variables  map[string]any          `json:"variables"`
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
		Meta:       persona.Config.Meta,
		Lore:       persona.Lore,
		Anchors:    persona.Anchors,
		Generation: persona.Config.Generation,
		Variables:  persona.Config.Variables,
	}

	writeJSON(w, http.StatusOK, detail)
}

func (h *Handlers) UpdatePersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := h.personaService.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "persona not found: "+id)
		return
	}

	var req PersonaDetail
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Preserve the original system_template — not editable from the web UI
	updated := &domain.PersonaModule{
		Config: domain.PersonaConfig{
			Meta:           req.Meta,
			SystemTemplate: existing.Config.SystemTemplate,
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

	if req.Message == "" || req.PersonaID == "" {
		writeError(w, http.StatusBadRequest, "message and persona_id are required")
		return
	}

	if req.ChannelID == "" {
		req.ChannelID = "web-test"
	}

	response, err := h.chatService.HandleMessage(r.Context(), req.ChannelID, req.PersonaID, req.Message)
	if err != nil {
		// Distinguish between configuration errors and actual server errors
		if strings.Contains(err.Error(), "LLM provider not configured") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

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

// --- Settings endpoints ---

type SettingsResponse struct {
	GeminiAPIKey string `json:"gemini_api_key"`
	GeminiModel  string `json:"gemini_model"`
	DiscordToken string `json:"discord_token"`
}

type SettingsUpdateRequest struct {
	GeminiAPIKey string `json:"gemini_api_key"`
	GeminiModel  string `json:"gemini_model"`
	DiscordToken string `json:"discord_token"`
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
	})
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Load existing settings to preserve unchanged masked fields
	existing, err := h.settingsStore.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load settings: "+err.Error())
		return
	}

	// Only update fields that are not masked placeholders
	if req.GeminiAPIKey != "" && !isMasked(req.GeminiAPIKey) {
		existing.GeminiAPIKey = req.GeminiAPIKey
	}
	if req.GeminiModel != "" {
		existing.GeminiModel = req.GeminiModel
	}
	if req.DiscordToken != "" && !isMasked(req.DiscordToken) {
		existing.DiscordToken = req.DiscordToken
	}

	if err := h.settingsStore.Save(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "save settings: "+err.Error())
		return
	}

	// Hot-swap the LLM provider if API key is available
	if existing.GeminiAPIKey != "" {
		model := existing.GeminiModel
		if model == "" {
			model = "gemini-2.5-pro-preview-06-05"
		}

		provider, err := llm.NewGeminiAPIProvider(h.ctx, existing.GeminiAPIKey, model)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid API key: "+err.Error())
			return
		}
		h.providerManager.Update(provider)
		log.Printf("provider hot-swapped: %s (model: %s)", provider.ModelInfo().ProviderName, model)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *Handlers) GetProviderStatus(w http.ResponseWriter, r *http.Request) {
	info := h.providerManager.ModelInfo()
	writeJSON(w, http.StatusOK, ProviderStatusResponse{
		Configured:   h.providerManager.IsConfigured(),
		ProviderName: info.ProviderName,
		Model:        info.ProviderName, // ModelInfo doesn't expose model name separately
	})
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
