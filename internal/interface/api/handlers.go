package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	schedulerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/scheduler"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	discordruntime "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/mcp"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logAPI = logger.New("api", logger.Cyan)

// Handlers implements the REST API endpoints.
type Handlers struct {
	personaService         *app.PersonaService
	chatService            *app.ChatService
	schedulerService       *schedulerpkg.Service
	settingsStore          *config.SettingsStore
	providerManager        *app.ProviderManager
	mcpManager             *mcp.Manager
	discordRuntime         *discordruntime.Runtime
	discordPersona         DiscordPersonaController
	oauthFlow              *llm.GeminiOAuthFlow
	convStore              store.ConversationRepository
	imageStore             *store.ImageStore
	ctx                    context.Context
	settingsRuntimeApplier *SettingsRuntimeApplier
}

type DiscordPersonaController interface {
	PersonaID() string
	SetPersonaID(id string)
}

func NewHandlers(
	personaService *app.PersonaService,
	chatService *app.ChatService,
	schedulerService *schedulerpkg.Service,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	mcpManager *mcp.Manager,
	discordRuntime *discordruntime.Runtime,
	discordPersona DiscordPersonaController,
	oauthFlow *llm.GeminiOAuthFlow,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	ctx context.Context,
) *Handlers {
	h := &Handlers{
		personaService:   personaService,
		chatService:      chatService,
		schedulerService: schedulerService,
		settingsStore:    settingsStore,
		providerManager:  providerManager,
		mcpManager:       mcpManager,
		discordRuntime:   discordRuntime,
		discordPersona:   discordPersona,
		oauthFlow:        oauthFlow,
		convStore:        convStore,
		imageStore:       imageStore,
		ctx:              ctx,
	}
	h.settingsRuntimeApplier = NewSettingsRuntimeApplier(h)
	return h
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
	ConfigYAML  string          `json:"config_yaml"`
	Lore        string          `json:"lore"`
	UserProfile string          `json:"user_profile"`
	Anchors     []domain.Anchor `json:"anchors"`
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

	configYAML, err := h.personaService.GetConfigRaw(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load persona config: "+err.Error())
		return
	}

	detail := PersonaDetail{
		ConfigYAML:  configYAML,
		Lore:        persona.Lore,
		UserProfile: persona.UserProfile,
		Anchors:     persona.Anchors,
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

	cfg, err := parseAndValidatePersonaConfigYAML(req.ConfigYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := &domain.PersonaModule{
		Config:      cfg,
		Lore:        req.Lore,
		UserProfile: req.UserProfile,
		Anchors:     req.Anchors,
	}

	if err := h.personaService.UpdateWithRawConfig(id, req.ConfigYAML, updated); err != nil {
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
