package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// --- Conversation endpoints ---

type ConversationSummary struct {
	ID            string `json:"id"`
	ChannelID     string `json:"channel_id"`
	PersonaID     string `json:"persona_id"`
	PersonaName   string `json:"persona_name"`
	IsActive      bool   `json:"is_active"`
	MessageCount  int    `json:"message_count"`
	LastMessage   string `json:"last_message,omitempty"`
	LastTimestamp string `json:"last_timestamp"`
	UpdatedAt     string `json:"updated_at"`
}

type CreateConversationRequest struct {
	ChannelID string `json:"channel_id,omitempty"`
	PersonaID string `json:"persona_id"`
}

func (h *Handlers) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var req CreateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	personaID := strings.TrimSpace(req.PersonaID)
	if personaID == "" {
		writeError(w, http.StatusBadRequest, "persona_id is required")
		return
	}

	channelID := strings.TrimSpace(req.ChannelID)
	if channelID == "" {
		channelID = "web-test"
	}

	conv, err := h.convStore.CreateSession(h.ctx, channelID, personaID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create conversation: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, conv)
}

func (h *Handlers) ListConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := h.convStore.List(h.ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list conversations: "+err.Error())
		return
	}

	summaries := make([]ConversationSummary, 0, len(conversations))
	activeCache := make(map[string]string)
	for _, conv := range conversations {
		personaName := conv.PersonaID
		if h.personaService != nil {
			if persona, ok := h.personaService.Get(conv.PersonaID); ok {
				personaName = persona.Config.Meta.Name
			}
		}

		messageCount := len(conv.Messages)
		lastMessage := ""
		lastTimestamp := conv.UpdatedAt

		if messageCount > 0 {
			lastMsg := conv.Messages[messageCount-1]
			lastMessage = truncate(lastMsg.Content, 100)
			lastTimestamp = lastMsg.Timestamp
		} else {
			// Keep reset conversations visible in Web list.
			if conv.HistoryCutoffAt == nil {
				continue
			}
			lastMessage = "[對話已重置]"
			lastTimestamp = conv.HistoryCutoffAt.UTC()
		}

		pairKey := conv.ChannelID + ":" + conv.PersonaID
		activeID, ok := activeCache[pairKey]
		if !ok {
			id, activeErr := h.convStore.GetActiveID(h.ctx, conv.ChannelID, conv.PersonaID)
			if activeErr != nil {
				logAPI.Warnf("list conversations: get active id failed for %s/%s: %v", conv.ChannelID, conv.PersonaID, activeErr)
			}
			activeID = id
			activeCache[pairKey] = activeID
		}

		summary := ConversationSummary{
			ID:            conv.ID,
			ChannelID:     conv.ChannelID,
			PersonaID:     conv.PersonaID,
			PersonaName:   personaName,
			IsActive:      activeID != "" && activeID == conv.ID,
			MessageCount:  messageCount,
			LastMessage:   lastMessage,
			LastTimestamp: lastTimestamp.Format("2006-01-02 15:04:05"),
			UpdatedAt:     conv.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		summaries = append(summaries, summary)
	}

	writeJSON(w, http.StatusOK, summaries)
}

func (h *Handlers) GetConversation(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channelID")
	personaID := r.PathValue("personaID")

	conv, err := h.convStore.Get(h.ctx, channelID, personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get conversation: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, conv)
}

func (h *Handlers) GetConversationByID(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("conversationID")
	conv, err := h.convStore.GetByID(h.ctx, conversationID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found: "+conversationID)
			return
		}
		writeError(w, http.StatusInternalServerError, "get conversation: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, conv)
}

func (h *Handlers) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channelID")
	personaID := r.PathValue("personaID")

	conv, err := h.convStore.Get(h.ctx, channelID, personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get conversation: "+err.Error())
		return
	}
	h.cleanupConversationImages(conv)

	if err := h.convStore.Delete(h.ctx, channelID, personaID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete conversation: "+err.Error())
		return
	}

	newActive, err := h.convStore.CreateSession(h.ctx, channelID, personaID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create replacement session: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":              "deleted",
		"new_conversation_id": newActive.ID,
	})
}

func (h *Handlers) DeleteConversationByID(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("conversationID")
	conv, err := h.convStore.GetByID(h.ctx, conversationID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found: "+conversationID)
			return
		}
		writeError(w, http.StatusInternalServerError, "get conversation: "+err.Error())
		return
	}

	activeID, err := h.convStore.GetActiveID(h.ctx, conv.ChannelID, conv.PersonaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get active conversation: "+err.Error())
		return
	}
	wasActive := activeID != "" && activeID == conv.ID

	h.cleanupConversationImages(conv)

	if err := h.convStore.DeleteByID(h.ctx, conversationID); err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found: "+conversationID)
			return
		}
		writeError(w, http.StatusInternalServerError, "delete conversation: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"status":     "deleted",
		"was_active": wasActive,
		"deleted_id": conversationID,
		"channel_id": conv.ChannelID,
		"persona_id": conv.PersonaID,
	}
	if wasActive {
		newActive, createErr := h.convStore.CreateSession(h.ctx, conv.ChannelID, conv.PersonaID, time.Now().UTC())
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "create replacement session: "+createErr.Error())
			return
		}
		resp["new_conversation_id"] = newActive.ID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) cleanupConversationImages(conv *domain.Conversation) {
	if h.imageStore == nil || conv == nil {
		return
	}
	var keys []string
	for _, msg := range conv.Messages {
		for _, ref := range msg.ImageRefs {
			keys = append(keys, ref.StorageKey)
		}
	}
	if len(keys) == 0 {
		return
	}
	h.imageStore.DeleteKeys(keys)
	logAPI.Logf("delete-conv: cleaned up %d image(s)", len(keys))
}
