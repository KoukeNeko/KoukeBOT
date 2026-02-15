package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/mcp"
)

type MCPDiscoverRequest struct {
	Server config.MCPServerSetting `json:"server"`
}

type MCPDiscoverResponse struct {
	Tools []mcp.ToolDescriptor `json:"tools"`
}

func (h *Handlers) DiscoverMCPServer(w http.ResponseWriter, r *http.Request) {
	if h.mcpManager == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP manager is not configured")
		return
	}

	var req MCPDiscoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	settings, err := h.settingsStore.Load()
	if err == nil {
		resolveMaskedMCPServerSecrets(&req.Server, settings.MCP)
	}

	if err := validateMCPServerSetting(req.Server); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	timeoutSeconds := config.DefaultMCPDiscoveryTimeoutSeconds
	if settings.MCP.DiscoveryTimeoutSeconds > 0 {
		timeoutSeconds = settings.MCP.DiscoveryTimeoutSeconds
	}
	tools, err := h.mcpManager.DiscoverServer(r.Context(), req.Server, time.Duration(timeoutSeconds)*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "MCP discovery failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, MCPDiscoverResponse{Tools: tools})
}

func (h *Handlers) GetMCPStatus(w http.ResponseWriter, r *http.Request) {
	if h.mcpManager == nil {
		writeJSON(w, http.StatusOK, []mcp.ServerStatus{})
		return
	}
	writeJSON(w, http.StatusOK, h.mcpManager.StatusSnapshot())
}
