package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
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
	ctx context.Context,
) *Router {
	handlers := NewHandlers(personaService, chatService, settingsStore, providerManager, ctx)
	mux := http.NewServeMux()

	r := &Router{mux: mux, handlers: handlers}
	r.registerRoutes()
	return r
}

func (r *Router) registerRoutes() {
	// Persona endpoints
	r.mux.HandleFunc("GET /api/personas", r.handlers.ListPersonas)
	r.mux.HandleFunc("GET /api/personas/{id}", r.handlers.GetPersona)
	r.mux.HandleFunc("POST /api/personas/reload", r.handlers.ReloadPersonas)

	// Chat endpoint (for web-based testing)
	r.mux.HandleFunc("POST /api/chat", r.handlers.Chat)

	// Settings
	r.mux.HandleFunc("GET /api/settings", r.handlers.GetSettings)
	r.mux.HandleFunc("PUT /api/settings", r.handlers.UpdateSettings)
	r.mux.HandleFunc("GET /api/settings/status", r.handlers.GetProviderStatus)

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
