package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// Router sets up the HTTP API routes.
type Router struct {
	mux      *http.ServeMux
	handlers *Handlers
}

func NewRouter(
	personaService *app.PersonaService,
	chatService *app.ChatService,
	settingsStore *config.SettingsStore,
	providerManager *app.ProviderManager,
	oauthFlow *llm.GeminiOAuthFlow,
	convStore store.ConversationRepository,
	ctx context.Context,
) *Router {
	handlers := NewHandlers(personaService, chatService, settingsStore, providerManager, oauthFlow, convStore, ctx)
	mux := http.NewServeMux()

	r := &Router{mux: mux, handlers: handlers}
	r.registerRoutes()
	return r
}

func (r *Router) registerRoutes() {
	// Persona endpoints
	r.mux.HandleFunc("GET /api/personas", r.handlers.ListPersonas)
	r.mux.HandleFunc("GET /api/personas/{id}", r.handlers.GetPersona)
	r.mux.HandleFunc("PUT /api/personas/{id}", r.handlers.UpdatePersona)
	r.mux.HandleFunc("POST /api/personas/reload", r.handlers.ReloadPersonas)

	// Chat endpoints (for web-based testing)
	r.mux.HandleFunc("POST /api/chat", r.handlers.Chat)
	r.mux.HandleFunc("GET /api/chat/stream", r.handlers.ChatStream)

	// Conversations
	r.mux.HandleFunc("GET /api/conversations", r.handlers.ListConversations)
	r.mux.HandleFunc("GET /api/conversations/{channelID}/{personaID}", r.handlers.GetConversation)
	r.mux.HandleFunc("DELETE /api/conversations/{channelID}/{personaID}", r.handlers.DeleteConversation)

	// Settings
	r.mux.HandleFunc("GET /api/settings", r.handlers.GetSettings)
	r.mux.HandleFunc("PUT /api/settings", r.handlers.UpdateSettings)
	r.mux.HandleFunc("GET /api/settings/status", r.handlers.GetProviderStatus)
	r.mux.HandleFunc("GET /api/models", r.handlers.ListModels)

	// OAuth
	r.mux.HandleFunc("POST /api/oauth/gemini/detect", r.handlers.OAuthDetect)
	r.mux.HandleFunc("POST /api/oauth/gemini/start", r.handlers.OAuthStart)
	r.mux.HandleFunc("GET /api/oauth/gemini/callback", r.handlers.OAuthCallback)
	r.mux.HandleFunc("GET /api/oauth/gemini/status", r.handlers.OAuthStatus)

	// Health
	r.mux.HandleFunc("GET /api/health", r.handlers.Health)
}

// Handler returns the http.Handler for use with http.Server.
func (r *Router) Handler() http.Handler {
	return withCORS(withLogging(r.mux))
}

// withCORS adds permissive CORS headers for development.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// withLogging logs each request.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HTTP %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
