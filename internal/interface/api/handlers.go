package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// Handlers implements the REST API endpoints.
type Handlers struct {
	personaService *app.PersonaService
	chatService    *app.ChatService
}

func NewHandlers(personaService *app.PersonaService, chatService *app.ChatService) *Handlers {
	return &Handlers{
		personaService: personaService,
		chatService:    chatService,
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

	// Return full detail including lore and anchors
	detail := map[string]any{
		"meta":       persona.Config.Meta,
		"lore":       persona.Lore,
		"anchors":    persona.Anchors,
		"generation": persona.Config.Generation,
		"variables":  persona.Config.Variables,
	}

	writeJSON(w, http.StatusOK, detail)
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
