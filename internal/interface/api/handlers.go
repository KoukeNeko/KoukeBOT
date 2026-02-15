package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logAPI = logger.New("api", logger.Cyan)

// Handlers implements the REST API endpoints.
type Handlers struct {
	personaService  *app.PersonaService
	chatService     *app.ChatService
	settingsStore   *config.SettingsStore
	providerManager *app.ProviderManager
	discordPersona  DiscordPersonaController
	oauthFlow       *llm.GeminiOAuthFlow
	convStore       store.ConversationRepository
	imageStore      *store.ImageStore
	ctx             context.Context
}

type DiscordPersonaController interface {
	PersonaID() string
	SetPersonaID(id string)
}

func NewHandlers(
	personaService *app.PersonaService,
	chatService *app.ChatService,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	discordPersona DiscordPersonaController,
	oauthFlow *llm.GeminiOAuthFlow,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	ctx context.Context,
) *Handlers {
	return &Handlers{
		personaService:  personaService,
		chatService:     chatService,
		settingsStore:   settingsStore,
		providerManager: providerManager,
		discordPersona:  discordPersona,
		oauthFlow:       oauthFlow,
		convStore:       convStore,
		imageStore:      imageStore,
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
	UserProfile    string                  `json:"user_profile"`
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
		UserProfile:    persona.UserProfile,
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
		Lore:        req.Lore,
		UserProfile: req.UserProfile,
		Anchors:     req.Anchors,
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
	ConversationID string `json:"conversation_id,omitempty"`
	ChannelID      string `json:"channel_id"`
	PersonaID      string `json:"persona_id"`
	Message        string `json:"message"`
}

type ChatResponseBody struct {
	Response string `json:"response"`
}

func (h *Handlers) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := llm.WithRequestContext(r.Context(), llm.RequestContext{Source: llm.RequestSourceAPI})

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	conversationID, channelID, personaID, err := h.resolveConversationRouting(ctx, req.ConversationID, req.ChannelID, req.PersonaID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	logAPI.Logf("chat: channel=%s persona=%s conversation=%s msg=%s",
		channelID, personaID, conversationID, truncate(req.Message, 50))

	var response string
	if conversationID != "" {
		response, err = h.chatService.HandleMessageByConversationID(ctx, conversationID, req.Message)
	} else {
		response, err = h.chatService.HandleMessage(ctx, channelID, personaID, req.Message)
	}
	if err != nil {
		logAPI.Errorf("chat: %v", err)
		// Distinguish between configuration errors and actual server errors
		if strings.Contains(err.Error(), "LLM provider not configured") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	logAPI.Logf("chat: response length=%d chars", len(response))
	writeJSON(w, http.StatusOK, ChatResponseBody{Response: response})
}

// ChatStream handles Server-Sent Events streaming for real-time chat responses.
// GET /api/chat/stream?persona_id=xxx&message=xxx&channel_id=web-test&conversation_id=... (text only)
// POST /api/chat/stream with multipart/form-data (text + images + optional conversation_id)
func (h *Handlers) ChatStream(w http.ResponseWriter, r *http.Request) {
	ctx := llm.WithRequestContext(r.Context(), llm.RequestContext{Source: llm.RequestSourceAPI})

	var personaID, message, channelID, conversationID string
	var images []domain.ImageInput

	// Parse request based on method
	if r.Method == http.MethodGet {
		// GET: Query params (text only)
		personaID = r.URL.Query().Get("persona_id")
		message = r.URL.Query().Get("message")
		channelID = r.URL.Query().Get("channel_id")
		conversationID = r.URL.Query().Get("conversation_id")
	} else if r.Method == http.MethodPost {
		// POST: Multipart form data (text + images)
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB max
			writeError(w, http.StatusBadRequest, "parse multipart form: "+err.Error())
			return
		}

		personaID = r.FormValue("persona_id")
		message = r.FormValue("message")
		channelID = r.FormValue("channel_id")
		conversationID = r.FormValue("conversation_id")

		// Parse images
		if files := r.MultipartForm.File["images"]; len(files) > 0 {
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					writeError(w, http.StatusBadRequest, "open image file: "+err.Error())
					return
				}
				defer file.Close()

				// Read file content
				data := make([]byte, fileHeader.Size)
				if _, err := file.Read(data); err != nil {
					writeError(w, http.StatusBadRequest, "read image file: "+err.Error())
					return
				}

				// Validate MIME type
				mimeType := fileHeader.Header.Get("Content-Type")
				if !isValidImageMIME(mimeType) {
					writeError(w, http.StatusBadRequest, "invalid image type: "+mimeType)
					return
				}

				images = append(images, domain.ImageInput{
					Data:     data,
					MimeType: mimeType,
					Filename: fileHeader.Filename,
				})
			}
		}
	} else {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if message == "" {
		writeError(w, http.StatusBadRequest, "missing message")
		return
	}

	conversationID, channelID, personaID, err := h.resolveConversationRouting(ctx, conversationID, channelID, personaID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	logAPI.Logf("chat-stream: channel=%s persona=%s conversation=%s msg=%s images=%d",
		channelID, personaID, conversationID, truncate(message, 50), len(images))

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Start streaming (with or without images)
	var streamCh <-chan llm.StreamChunk
	if len(images) > 0 {
		if conversationID != "" {
			streamCh, err = h.chatService.HandleMessageStreamWithImagesByConversationID(ctx, conversationID, message, images)
		} else {
			streamCh, err = h.chatService.HandleMessageStreamWithImages(ctx, channelID, personaID, message, images)
		}
	} else {
		if conversationID != "" {
			streamCh, err = h.chatService.HandleMessageStreamByConversationID(ctx, conversationID, message)
		} else {
			streamCh, err = h.chatService.HandleMessageStream(ctx, channelID, personaID, message)
		}
	}

	if err != nil {
		logAPI.Errorf("chat-stream: init error: %v", err)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Forward chunks as SSE events
	totalChunks := 0
	for chunk := range streamCh {
		if chunk.Error != nil {
			logAPI.Errorf("chat-stream: %v", chunk.Error)
			// Format user-friendly error message
			userFriendlyMsg := formatUserFriendlyError(chunk.Error)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", userFriendlyMsg)
			flusher.Flush()
			return
		}

		// Handle tool status events
		if chunk.ToolStatus != "" {
			toolData := map[string]interface{}{
				"status": chunk.ToolStatus,
				"name":   chunk.ToolName,
			}
			if chunk.ToolInput != nil {
				toolData["input"] = chunk.ToolInput
			}
			if chunk.ToolError != "" {
				toolData["error"] = chunk.ToolError
			}
			toolJSON, _ := json.Marshal(toolData)
			fmt.Fprintf(w, "event: tool\ndata: %s\n\n", string(toolJSON))
			flusher.Flush()
			continue
		}

		// Regular content chunk
		if chunk.Content != "" {
			// Escape newlines for SSE format
			content := strings.ReplaceAll(chunk.Content, "\n", "\\n")
			fmt.Fprintf(w, "data: %s\n\n", content)
			flusher.Flush()
			totalChunks++
		}
	}

	// Send done event
	logAPI.Logf("chat-stream: streamed %d chunks", totalChunks)
	fmt.Fprintf(w, "event: done\ndata: \n\n")
	flusher.Flush()
}

func (h *Handlers) resolveConversationRouting(ctx context.Context, conversationID, channelID, personaID string) (resolvedConversationID, resolvedChannelID, resolvedPersonaID string, err error) {
	if strings.TrimSpace(conversationID) == "" {
		if channelID == "" {
			channelID = "web-test"
		}
		if personaID == "" {
			return "", "", "", fmt.Errorf("missing persona_id or conversation_id")
		}
		return "", channelID, personaID, nil
	}

	conv, err := h.convStore.GetByID(ctx, conversationID)
	if err != nil {
		return "", "", "", fmt.Errorf("get conversation %s: %w", conversationID, err)
	}
	if conv == nil {
		return "", "", "", fmt.Errorf("%w: %s", store.ErrConversationNotFound, conversationID)
	}
	if personaID != "" && personaID != conv.PersonaID {
		return "", "", "", fmt.Errorf("conversation_id/persona_id mismatch: %s != %s", conv.PersonaID, personaID)
	}
	if channelID != "" && channelID != conv.ChannelID {
		return "", "", "", fmt.Errorf("conversation_id/channel_id mismatch: %s != %s", conv.ChannelID, channelID)
	}
	return conv.ID, conv.ChannelID, conv.PersonaID, nil
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

// formatUserFriendlyError converts technical errors to user-friendly messages
func formatUserFriendlyError(err error) string {
	errMsg := err.Error()

	// Check for quota exhausted (429) error
	if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "QUOTA_EXHAUSTED") || strings.Contains(errMsg, "exhausted your capacity") {
		// Extract reset time if available
		if strings.Contains(errMsg, "quota will reset after") {
			// Try to extract the time (e.g., "16m9s")
			start := strings.Index(errMsg, "reset after ") + len("reset after ")
			end := strings.Index(errMsg[start:], ".")
			if end > 0 {
				resetTime := errMsg[start : start+end]
				return fmt.Sprintf("⏳ API 配額已用完，請等待 %s 後重試\n\n💡 提示：可以切換到 API Key 模式或等待配額重置", resetTime)
			}
		}
		return "⏳ API 配額已用完，請稍後再試\n\n💡 提示：請至 Settings 切換到 API Key 模式，或等待配額重置"
	}

	// Check for authentication errors
	if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "Unauthorized") || strings.Contains(errMsg, "UNAUTHENTICATED") {
		return "🔐 認證失敗\n\n💡 提示：請至 Settings 重新設定 API Key 或 OAuth"
	}

	// Check for network errors
	if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "network") {
		return "🌐 網路連線失敗\n\n💡 提示：請檢查網路連線或稍後再試"
	}

	// Check for rate limit errors
	if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "too many requests") {
		return "⚠️ 請求過於頻繁\n\n💡 提示：請稍等片刻後再試"
	}

	// Generic error fallback
	return fmt.Sprintf("❌ 發生錯誤：%s\n\n💡 提示：請查看後端 log 或至 Settings 檢查設定", errMsg)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Settings endpoints ---

type SettingsResponse struct {
	GeminiAPIKey                    string `json:"gemini_api_key"`
	GeminiModel                     string `json:"gemini_model"`
	DiscordToken                    string `json:"discord_token"`
	ProviderType                    string `json:"provider_type"`
	BraveAPIKey                     string `json:"brave_api_key"`
	WebSearchEnabled                bool   `json:"web_search_enabled"`
	DiscordUseReply                 bool   `json:"discord_use_reply"`
	PerplexityAPIKey                string `json:"perplexity_api_key"` // OpenClaw 相容
	SearchProvider                  string `json:"search_provider"`    // OpenClaw 相容
	WebFetchEnabled                 bool   `json:"web_fetch_enabled"`  // OpenClaw 相容
	Timezone                        string `json:"timezone"`           // OpenClaw 相容
	TimeFormat                      string `json:"time_format"`        // OpenClaw 相容
	Location                        string `json:"location"`           // OpenClaw 相容
	ContextFlushEnabled             bool   `json:"context_flush_enabled"`
	ContextFlushThreshold           int    `json:"context_flush_threshold"`
	ContextWindowSize               int    `json:"context_window_size"`
	LogChannelID                    string `json:"log_channel_id"` // Discord channel ID for stdout logs
	DiscordPersonaID                string `json:"discord_persona_id"`
	MemoryExtractionEnabled         bool   `json:"memory_extraction_enabled"`
	MemoryExtractionModel           string `json:"memory_extraction_model"`
	PersonaLockEveryTurns           int    `json:"persona_lock_every_turns"`
	TripletOutputEnabled            bool   `json:"triplet_output_enabled"`
	AnalysisVisibility              string `json:"analysis_visibility"`
	NegativeConstraintEnabled       bool   `json:"negative_constraint_enabled"`
	NegativeConstraintDefault       string `json:"negative_constraint_default"`
	DiscordObserveEnabled           bool   `json:"discord_observe_enabled"`
	DiscordMemberIntentEnabled      bool   `json:"discord_member_intent_enabled"`
	DiscordPresenceIntentEnabled    bool   `json:"discord_presence_intent_enabled"`
	DiscordObserveMaxResults        int    `json:"discord_observe_max_results"`
	DiscordObserveMessageScope      string `json:"discord_observe_message_scope"`
	MaxToolRounds                   int    `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        int    `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  bool   `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    bool   `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds int    `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   bool   `json:"discord_show_internal_tool_errors"`
}

type SettingsUpdateRequest struct {
	GeminiAPIKey                    string `json:"gemini_api_key"`
	GeminiModel                     string `json:"gemini_model"`
	DiscordToken                    string `json:"discord_token"`
	ProviderType                    string `json:"provider_type"`
	BraveAPIKey                     string `json:"brave_api_key"`
	WebSearchEnabled                *bool  `json:"web_search_enabled"` // Pointer to distinguish false from unset
	DiscordUseReply                 *bool  `json:"discord_use_reply"`  // Pointer to distinguish false from unset
	PerplexityAPIKey                string `json:"perplexity_api_key"` // OpenClaw 相容
	SearchProvider                  string `json:"search_provider"`    // OpenClaw 相容
	WebFetchEnabled                 *bool  `json:"web_fetch_enabled"`  // OpenClaw 相容
	Timezone                        string `json:"timezone"`           // OpenClaw 相容
	TimeFormat                      string `json:"time_format"`        // OpenClaw 相容
	Location                        string `json:"location"`           // OpenClaw 相容
	ContextFlushEnabled             *bool  `json:"context_flush_enabled"`
	ContextFlushThreshold           *int   `json:"context_flush_threshold"`
	ContextWindowSize               *int   `json:"context_window_size"`
	LogChannelID                    string `json:"log_channel_id"` // Discord channel ID for stdout logs
	DiscordPersonaID                string `json:"discord_persona_id"`
	MemoryExtractionEnabled         *bool  `json:"memory_extraction_enabled"`
	MemoryExtractionModel           string `json:"memory_extraction_model"`
	PersonaLockEveryTurns           *int   `json:"persona_lock_every_turns"`
	TripletOutputEnabled            *bool  `json:"triplet_output_enabled"`
	AnalysisVisibility              string `json:"analysis_visibility"`
	NegativeConstraintEnabled       *bool  `json:"negative_constraint_enabled"`
	NegativeConstraintDefault       string `json:"negative_constraint_default"`
	DiscordObserveEnabled           *bool  `json:"discord_observe_enabled"`
	DiscordMemberIntentEnabled      *bool  `json:"discord_member_intent_enabled"`
	DiscordPresenceIntentEnabled    *bool  `json:"discord_presence_intent_enabled"`
	DiscordObserveMaxResults        *int   `json:"discord_observe_max_results"`
	DiscordObserveMessageScope      string `json:"discord_observe_message_scope"`
	MaxToolRounds                   *int   `json:"max_tool_rounds"`
	MaxWebSearchCallsPerTurn        *int   `json:"max_web_search_calls_per_turn"`
	ForceFinalAnswerAfterToolLimit  *bool  `json:"force_final_answer_after_tool_limit"`
	TransientRecoveryWaitEnabled    *bool  `json:"transient_recovery_wait_enabled"`
	TransientRecoveryWaitMaxSeconds *int   `json:"transient_recovery_wait_max_seconds"`
	DiscordShowInternalToolErrors   *bool  `json:"discord_show_internal_tool_errors"`
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

	// 確保 SearchProvider 有預設值
	searchProvider := settings.SearchProvider
	if searchProvider == "" {
		searchProvider = "auto"
	}
	discordPersonaID := settings.DiscordPersonaID
	if discordPersonaID == "" && h.discordPersona != nil {
		discordPersonaID = h.discordPersona.PersonaID()
	}

	writeJSON(w, http.StatusOK, SettingsResponse{
		GeminiAPIKey:                    maskKey(settings.GeminiAPIKey),
		GeminiModel:                     settings.GeminiModel,
		DiscordToken:                    maskKey(settings.DiscordToken),
		ProviderType:                    settings.ProviderType,
		BraveAPIKey:                     maskKey(settings.BraveAPIKey),
		WebSearchEnabled:                settings.WebSearchEnabled,
		DiscordUseReply:                 settings.DiscordUseReply,
		PerplexityAPIKey:                maskKey(settings.PerplexityAPIKey),
		SearchProvider:                  searchProvider,
		WebFetchEnabled:                 settings.WebFetchEnabled,
		Timezone:                        settings.Timezone,
		TimeFormat:                      settings.TimeFormat,
		Location:                        settings.Location,
		ContextFlushEnabled:             settings.ContextFlushEnabled,
		ContextFlushThreshold:           settings.ContextFlushThreshold,
		ContextWindowSize:               settings.ContextWindowSize,
		LogChannelID:                    settings.LogChannelID,
		DiscordPersonaID:                discordPersonaID,
		MemoryExtractionEnabled:         settings.MemoryExtractionEnabled,
		MemoryExtractionModel:           settings.MemoryExtractionModel,
		PersonaLockEveryTurns:           settings.PersonaLockEveryTurns,
		TripletOutputEnabled:            settings.TripletOutputEnabled,
		AnalysisVisibility:              settings.AnalysisVisibility,
		NegativeConstraintEnabled:       settings.NegativeConstraintEnabled,
		NegativeConstraintDefault:       settings.NegativeConstraintDefault,
		DiscordObserveEnabled:           settings.DiscordObserveEnabled,
		DiscordMemberIntentEnabled:      settings.DiscordMemberIntentEnabled,
		DiscordPresenceIntentEnabled:    settings.DiscordPresenceIntentEnabled,
		DiscordObserveMaxResults:        settings.DiscordObserveMaxResults,
		DiscordObserveMessageScope:      settings.DiscordObserveMessageScope,
		MaxToolRounds:                   settings.MaxToolRounds,
		MaxWebSearchCallsPerTurn:        settings.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit:  settings.ForceFinalAnswerAfterToolLimit,
		TransientRecoveryWaitEnabled:    settings.TransientRecoveryWaitEnabled,
		TransientRecoveryWaitMaxSeconds: settings.TransientRecoveryWaitMaxSeconds,
		DiscordShowInternalToolErrors:   settings.DiscordShowInternalToolErrors,
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
	if req.BraveAPIKey != "" && !isMasked(req.BraveAPIKey) {
		existing.BraveAPIKey = req.BraveAPIKey
	}
	if req.WebSearchEnabled != nil {
		existing.WebSearchEnabled = *req.WebSearchEnabled
	}
	if req.DiscordUseReply != nil {
		existing.DiscordUseReply = *req.DiscordUseReply
	}
	if req.PerplexityAPIKey != "" && !isMasked(req.PerplexityAPIKey) {
		existing.PerplexityAPIKey = req.PerplexityAPIKey
	}
	// SearchProvider 總是更新（包括空字串和預設值）
	existing.SearchProvider = req.SearchProvider
	if req.WebFetchEnabled != nil {
		existing.WebFetchEnabled = *req.WebFetchEnabled
	}
	// Time/Location settings
	existing.Timezone = req.Timezone
	existing.TimeFormat = req.TimeFormat
	existing.Location = req.Location
	// Context flush settings
	if req.ContextFlushEnabled != nil {
		existing.ContextFlushEnabled = *req.ContextFlushEnabled
	}
	if req.ContextFlushThreshold != nil {
		existing.ContextFlushThreshold = *req.ContextFlushThreshold
	}
	if req.ContextWindowSize != nil {
		existing.ContextWindowSize = *req.ContextWindowSize
	}
	if req.PersonaLockEveryTurns != nil {
		existing.PersonaLockEveryTurns = *req.PersonaLockEveryTurns
	}
	if req.TripletOutputEnabled != nil {
		existing.TripletOutputEnabled = *req.TripletOutputEnabled
	}
	if req.NegativeConstraintEnabled != nil {
		existing.NegativeConstraintEnabled = *req.NegativeConstraintEnabled
	}
	if req.AnalysisVisibility != "" {
		existing.AnalysisVisibility = req.AnalysisVisibility
	}
	if req.NegativeConstraintDefault != "" {
		existing.NegativeConstraintDefault = req.NegativeConstraintDefault
	}
	if req.DiscordObserveEnabled != nil {
		existing.DiscordObserveEnabled = *req.DiscordObserveEnabled
	}
	if req.DiscordMemberIntentEnabled != nil {
		existing.DiscordMemberIntentEnabled = *req.DiscordMemberIntentEnabled
	}
	if req.DiscordPresenceIntentEnabled != nil {
		existing.DiscordPresenceIntentEnabled = *req.DiscordPresenceIntentEnabled
	}
	if req.DiscordObserveMaxResults != nil {
		existing.DiscordObserveMaxResults = *req.DiscordObserveMaxResults
	}
	if req.DiscordObserveMessageScope != "" {
		existing.DiscordObserveMessageScope = req.DiscordObserveMessageScope
	}
	if req.MaxToolRounds != nil {
		existing.MaxToolRounds = *req.MaxToolRounds
	}
	if req.MaxWebSearchCallsPerTurn != nil {
		existing.MaxWebSearchCallsPerTurn = *req.MaxWebSearchCallsPerTurn
	}
	if req.ForceFinalAnswerAfterToolLimit != nil {
		existing.ForceFinalAnswerAfterToolLimit = *req.ForceFinalAnswerAfterToolLimit
	}
	if req.TransientRecoveryWaitEnabled != nil {
		existing.TransientRecoveryWaitEnabled = *req.TransientRecoveryWaitEnabled
	}
	if req.TransientRecoveryWaitMaxSeconds != nil {
		existing.TransientRecoveryWaitMaxSeconds = *req.TransientRecoveryWaitMaxSeconds
	}
	if req.DiscordShowInternalToolErrors != nil {
		existing.DiscordShowInternalToolErrors = *req.DiscordShowInternalToolErrors
	}
	// Memory extraction settings
	if req.MemoryExtractionEnabled != nil {
		existing.MemoryExtractionEnabled = *req.MemoryExtractionEnabled
	}
	existing.MemoryExtractionModel = req.MemoryExtractionModel
	// Discord logging settings
	existing.LogChannelID = req.LogChannelID
	if req.DiscordPersonaID != "" {
		if _, ok := h.personaService.Get(req.DiscordPersonaID); !ok {
			writeError(w, http.StatusBadRequest, "invalid discord persona id: "+req.DiscordPersonaID)
			return
		}
		existing.DiscordPersonaID = req.DiscordPersonaID
	}
	if err := validatePromptGuardSettings(
		existing.PersonaLockEveryTurns,
		existing.AnalysisVisibility,
		existing.NegativeConstraintEnabled,
		existing.NegativeConstraintDefault,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDiscordObserveSettings(existing.DiscordObserveMaxResults, existing.DiscordObserveMessageScope); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateToolLoopSettings(
		existing.MaxToolRounds,
		existing.MaxWebSearchCallsPerTurn,
		existing.TransientRecoveryWaitMaxSeconds,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logAPI.Logf("settings: saving model=%s provider=%s websearch=%v reply=%v",
		existing.GeminiModel, existing.ProviderType, existing.WebSearchEnabled, existing.DiscordUseReply)

	if err := h.settingsStore.Save(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "save settings: "+err.Error())
		return
	}

	// Hot-swap active Discord persona before provider activation,
	// so persona switch succeeds even if provider activation fails.
	if h.discordPersona != nil && existing.DiscordPersonaID != "" {
		h.discordPersona.SetPersonaID(existing.DiscordPersonaID)
		logAPI.Logf("settings: discord persona switched to %s", existing.DiscordPersonaID)
	}

	h.chatService.UpdatePromptGuardPolicy(app.PromptGuardPolicy{
		PersonaLockEveryTurns:     existing.PersonaLockEveryTurns,
		TripletOutputEnabled:      existing.TripletOutputEnabled,
		AnalysisVisibility:        existing.AnalysisVisibility,
		NegativeConstraintEnabled: existing.NegativeConstraintEnabled,
		NegativeConstraintDefault: existing.NegativeConstraintDefault,
	})
	h.chatService.UpdateToolExecutionPolicy(app.ToolExecutionPolicy{
		MaxToolRounds:                  existing.MaxToolRounds,
		MaxWebSearchCallsPerTurn:       existing.MaxWebSearchCallsPerTurn,
		ForceFinalAnswerAfterToolLimit: existing.ForceFinalAnswerAfterToolLimit,
		DiscordShowInternalToolErrors:  existing.DiscordShowInternalToolErrors,
	})
	if pool := h.providerManager.GetPool(); pool != nil {
		pool.SetTransientRecoveryWait(
			existing.TransientRecoveryWaitEnabled,
			time.Duration(existing.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
	}

	// Hot-swap the LLM provider based on selected provider_type.
	// Provider activation failure should not block a successful save response,
	// because settings and persona switch already succeeded above.
	response := map[string]string{"status": "saved"}
	if err := h.activateProvider(existing); err != nil {
		logAPI.Warnf("settings: provider activation failed (settings saved): %v", err)
		response["warning"] = err.Error()
	}

	logAPI.Logf("settings: saved (warning=%v)", response["warning"] != "")
	writeJSON(w, http.StatusOK, response)
}

// activateProvider creates and hot-swaps the LLM provider based on settings.
func (h *Handlers) activateProvider(s config.Settings) error {
	model := s.GeminiModel
	if model == "" {
		model = "gemini-2.5-pro"
	}

	logAPI.Logf("activate-provider: type=%s model=%s", s.ProviderType, model)

	switch s.ProviderType {
	case config.ProviderOAuth:
		logAPI.Logf("activate-provider: switching to OAuth")
		// Load all pool tokens and build a ProviderPool (not just a single token)
		poolPath := h.oauthFlow.PoolPath()
		tokens, err := llm.LoadOAuthPool(poolPath)
		if err != nil || len(tokens) == 0 {
			// Fallback to single token
			oauthData, err := h.oauthFlow.GetStatus()
			if err != nil || oauthData == nil {
				return fmt.Errorf("no OAuth credentials available")
			}
			tokens = []*llm.OAuthTokenData{oauthData}
		}
		pool := app.NewProviderPool()
		pool.SetPoolStorePath(poolPath)
		if s.AutoRetryCount > 0 {
			pool.SetAutoRetryCount(s.AutoRetryCount)
		}
		pool.SetTransientRecoveryWait(
			s.TransientRecoveryWaitEnabled,
			time.Duration(s.TransientRecoveryWaitMaxSeconds)*time.Second,
		)
		for _, tokenData := range tokens {
			if err := llm.RefreshIfNeeded(h.ctx, tokenData, ""); err != nil {
				logAPI.Warnf("activate-provider: token refresh failed for %s: %v", tokenData.Email, err)
				continue
			}
			if poolPath != "" && tokenData.Email != "" {
				if err := llm.UpdateOAuthPoolToken(poolPath, tokenData.Email, tokenData); err != nil {
					logAPI.Warnf("activate-provider: persist refreshed token failed for %s: %v", tokenData.Email, err)
				}
			}
			p, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
			if err != nil {
				logAPI.Warnf("activate-provider: provider init failed for %s: %v", tokenData.Email, err)
				continue
			}
			pool.AddOrReplaceWithUsage(p, tokenData.Email, tokenData.UsageStats)
		}
		if pool.Size() == 0 {
			return fmt.Errorf("no OAuth accounts available")
		}
		h.providerManager.Update(pool)
		logAPI.Logf("provider hot-swapped: OAuth pool (%d accounts, model: %s)", pool.Size(), model)

	case config.ProviderAPIKey:
		if s.GeminiAPIKey == "" {
			return fmt.Errorf("no API key configured")
		}
		provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
		if err != nil {
			return fmt.Errorf("invalid API key: %w", err)
		}
		h.providerManager.Update(provider)
		logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)

	default:
		// Auto mode: try API key first, then OAuth.
		if s.GeminiAPIKey != "" {
			provider, err := llm.NewGeminiAPIProvider(h.ctx, s.GeminiAPIKey, model)
			if err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}
			h.providerManager.Update(provider)
			logAPI.Logf("provider hot-swapped: gemini-api (model: %s)", model)
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

func validatePromptGuardSettings(lockEvery int, analysisVisibility string, negativeConstraintEnabled bool, negativeConstraintDefault string) error {
	if lockEvery < 1 || lockEvery > 16 {
		return fmt.Errorf("persona_lock_every_turns must be between 1 and 16")
	}
	switch analysisVisibility {
	case app.AnalysisVisibilityVisible, app.AnalysisVisibilityHidden, app.AnalysisVisibilityDebug:
		// valid
	default:
		return fmt.Errorf("analysis_visibility must be one of: visible, hidden, debug")
	}
	if negativeConstraintEnabled && strings.TrimSpace(negativeConstraintDefault) == "" {
		return fmt.Errorf("negative_constraint_default cannot be empty when negative constraints are enabled")
	}
	return nil
}

func validateDiscordObserveSettings(maxResults int, messageScope string) error {
	if maxResults < 1 || maxResults > 100 {
		return fmt.Errorf("discord_observe_max_results must be between 1 and 100")
	}
	switch messageScope {
	case config.DiscordObserveScopeCurrentChannel, config.DiscordObserveScopeCurrentGuild:
		return nil
	default:
		return fmt.Errorf("discord_observe_message_scope must be one of: %s, %s",
			config.DiscordObserveScopeCurrentChannel, config.DiscordObserveScopeCurrentGuild)
	}
}

func validateToolLoopSettings(maxToolRounds, maxWebSearchCallsPerTurn, transientRecoveryWaitMaxSeconds int) error {
	if maxToolRounds < 1 || maxToolRounds > 6 {
		return fmt.Errorf("max_tool_rounds must be between 1 and 6")
	}
	if maxWebSearchCallsPerTurn < 1 || maxWebSearchCallsPerTurn > 4 {
		return fmt.Errorf("max_web_search_calls_per_turn must be between 1 and 4")
	}
	if transientRecoveryWaitMaxSeconds < 1 || transientRecoveryWaitMaxSeconds > 15 {
		return fmt.Errorf("transient_recovery_wait_max_seconds must be between 1 and 15")
	}
	return nil
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
	Mode         string `json:"mode"`         // "local" | "remote"
	RedirectURL  string `json:"redirect_url"` // optional override
}

type OAuthStartResponse struct {
	FlowID    string `json:"flow_id"`
	AuthURL   string `json:"auth_url"`
	ExpiresAt string `json:"expires_at"`
	Mode      string `json:"mode"`
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

	info, err := h.oauthFlow.StartFlowV2(clientID, clientSecret, mode, redirectURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start OAuth flow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, OAuthStartResponse{
		FlowID:    info.FlowID,
		AuthURL:   info.AuthURL,
		ExpiresAt: info.ExpiresAt.Format(time.RFC3339),
		Mode:      info.Mode,
	})
}

// completeOAuthCallback is the shared logic for both automatic and manual OAuth callbacks.
func (h *Handlers) completeOAuthCallback(flowID, code, state string) (*llm.OAuthTokenData, error) {
	tokenData, err := h.oauthFlow.HandleCallbackWithFlow(h.ctx, flowID, code, state)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	model := "gemini-2.5-pro"
	var loadedSettings config.Settings
	loadedSettings, _ = h.settingsStore.Load()
	if loadedSettings.GeminiModel != "" {
		model = loadedSettings.GeminiModel
	}

	provider, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
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

	tokenData, err := h.completeOAuthCallback(flowID, code, state)
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

	tokenData, err := h.completeOAuthCallback(req.FlowID, code, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":      tokenData.Email,
		"project_id": tokenData.ProjectID,
		"status":     "ok",
	})
}

// OAuthComplete handles explicit flow completion for remote fallback.
func (h *Handlers) OAuthComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FlowID      string `json:"flow_id"`
		CallbackURL string `json:"callback_url"`
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

	tokenData, err := h.completeOAuthCallback(req.FlowID, code, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":      tokenData.Email,
		"project_id": tokenData.ProjectID,
		"status":     "ok",
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

	model := "gemini-2.5-pro"
	if s, loadErr := h.settingsStore.Load(); loadErr == nil && s.GeminiModel != "" {
		model = s.GeminiModel
	}

	provider, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
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
		_ = h.settingsStore.Save(s)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":      tokenData.Email,
		"project_id": tokenData.ProjectID,
		"status":     "ok",
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

	model := "gemini-2.5-pro"
	if s, loadErr := h.settingsStore.Load(); loadErr == nil && s.GeminiModel != "" {
		model = s.GeminiModel
	}

	provider, err := llm.NewGeminiOAuthProvider(h.ctx, tokenData, model)
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
		_ = h.settingsStore.Save(s)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"email":      tokenData.Email,
		"project_id": tokenData.ProjectID,
		"status":     "ok",
	})
}

type OAuthStatusResponse struct {
	Authenticated bool             `json:"authenticated"`
	ProjectID     string           `json:"project_id,omitempty"`
	Email         string           `json:"email,omitempty"`
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

// isValidImageMIME checks if the MIME type is a supported image format.
func isValidImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
