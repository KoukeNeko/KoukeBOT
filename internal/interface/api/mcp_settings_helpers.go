package api

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

const maxMCPServers = 16

var mcpServerIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func validateMCPSettings(settings config.MCPSettings) error {
	if settings.DiscoveryTimeoutSeconds < config.MinMCPDiscoveryTimeoutSeconds || settings.DiscoveryTimeoutSeconds > config.MaxMCPDiscoveryTimeoutSeconds {
		return fmt.Errorf("mcp.discovery_timeout_seconds must be between %d and %d", config.MinMCPDiscoveryTimeoutSeconds, config.MaxMCPDiscoveryTimeoutSeconds)
	}
	if settings.ToolCallTimeoutSeconds < config.MinMCPToolCallTimeoutSeconds || settings.ToolCallTimeoutSeconds > config.MaxMCPToolCallTimeoutSeconds {
		return fmt.Errorf("mcp.tool_call_timeout_seconds must be between %d and %d", config.MinMCPToolCallTimeoutSeconds, config.MaxMCPToolCallTimeoutSeconds)
	}
	if len(settings.Servers) > maxMCPServers {
		return fmt.Errorf("mcp.servers must be <= %d", maxMCPServers)
	}

	seen := make(map[string]struct{}, len(settings.Servers))
	for _, server := range settings.Servers {
		if err := validateMCPServerSetting(server); err != nil {
			return err
		}
		if _, exists := seen[server.ID]; exists {
			return fmt.Errorf("mcp server id duplicated: %s", server.ID)
		}
		seen[server.ID] = struct{}{}
	}

	return nil
}

func validateMCPServerSetting(server config.MCPServerSetting) error {
	id := strings.TrimSpace(server.ID)
	if !mcpServerIDPattern.MatchString(id) {
		return fmt.Errorf("mcp server id %q is invalid, expected [a-z0-9_-]{1,32}", server.ID)
	}

	switch strings.TrimSpace(server.Transport) {
	case "", mcpTransportStdio:
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("mcp server %s command is required for stdio transport", id)
		}
	case mcpTransportHTTP, mcpTransportSSE:
		if err := validateMCPHTTPURL(server.URL); err != nil {
			return fmt.Errorf("mcp server %s url: %w", id, err)
		}
	default:
		return fmt.Errorf("mcp server %s transport must be one of: stdio, http, sse", id)
	}

	for _, name := range server.ToolAllowlist {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("mcp server %s tool_allowlist contains empty name", id)
		}
	}
	if len(server.Instructions) > 2000 {
		return fmt.Errorf("mcp server %s instructions too long (%d chars, max 2000)", id, len(server.Instructions))
	}
	return nil
}

func validateMCPHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("URL must include scheme and host")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("scheme must be https or http")
	}
	if u.Scheme == "http" && !isPrivateOrLoopbackHost(u.Hostname()) {
		return fmt.Errorf("http is only allowed for localhost, loopback, or private network hosts")
	}
	return nil
}

func isPrivateOrLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return ip.IsPrivate()
}

func resolveMaskedMCPServerSecrets(incoming *config.MCPServerSetting, existing config.MCPSettings) {
	if incoming == nil || strings.TrimSpace(incoming.ID) == "" {
		return
	}
	var old *config.MCPServerSetting
	for idx := range existing.Servers {
		if existing.Servers[idx].ID == incoming.ID {
			old = &existing.Servers[idx]
			break
		}
	}
	if old == nil {
		return
	}
	incoming.Headers = mergeMaskedMap(incoming.Headers, old.Headers)
	incoming.Env = mergeMaskedMap(incoming.Env, old.Env)
}

func mergeMaskedMap(incoming, existing map[string]string) map[string]string {
	if len(incoming) == 0 {
		return nil
	}
	out := make(map[string]string, len(incoming))
	for key, value := range incoming {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		trimmedValue := strings.TrimSpace(value)
		if isMasked(trimmedValue) {
			if oldValue, ok := existing[trimmedKey]; ok {
				out[trimmedKey] = oldValue
				continue
			}
			trimmedValue = ""
		}
		out[trimmedKey] = trimmedValue
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	mcpTransportStdio = "stdio"
	mcpTransportHTTP  = "http"
	mcpTransportSSE   = "sse"
)
