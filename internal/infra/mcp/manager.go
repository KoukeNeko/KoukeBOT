package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logMCP = logger.New("MCP", logger.Cyan)

type Manager struct {
	mu              sync.RWMutex
	settings        config.MCPSettings
	servers         map[string]config.MCPServerSetting
	statuses        map[string]ServerStatus
	mcpInstructions []string // per-server instructions collected during BuildTools
	httpClient      *http.Client
}

func NewManager() *Manager {
	return &Manager{
		servers:    make(map[string]config.MCPServerSetting),
		statuses:   make(map[string]ServerStatus),
		httpClient: &http.Client{},
	}
}

func (m *Manager) Configure(settings config.MCPSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.settings = settings
	nextServers := make(map[string]config.MCPServerSetting, len(settings.Servers))
	nextStatuses := make(map[string]ServerStatus, len(settings.Servers))
	for _, server := range settings.Servers {
		nextServers[server.ID] = server
		prev := m.statuses[server.ID]
		nextStatuses[server.ID] = ServerStatus{
			ServerID:        server.ID,
			Enabled:         server.Enabled,
			Transport:       server.Transport,
			Reachable:       prev.Reachable,
			LastError:       prev.LastError,
			DiscoveredTools: prev.DiscoveredTools,
			RegisteredTools: prev.RegisteredTools,
			UpdatedAt:       prev.UpdatedAt,
		}
	}
	m.servers = nextServers
	m.statuses = nextStatuses
}

func (m *Manager) BuildTools(ctx context.Context, settings config.MCPSettings) ([]domain.Tool, []string) {
	m.Configure(settings)

	m.mu.Lock()
	m.mcpInstructions = nil
	m.mu.Unlock()

	if !settings.Enabled {
		m.resetRegistrations()
		return nil, nil
	}

	tools := make([]domain.Tool, 0)
	warnings := make([]string, 0)

	for _, server := range settings.Servers {
		if !server.Enabled {
			m.updateStatus(server.ID, func(status *ServerStatus) {
				status.Enabled = false
				status.Reachable = false
				status.RegisteredTools = nil
				status.UpdatedAt = time.Now().UTC()
			})
			continue
		}

		timeout := time.Duration(settings.DiscoveryTimeoutSeconds) * time.Second
		discoverCtx, cancel := context.WithTimeout(ctx, timeout)
		discovered, err := m.discoverWithServer(discoverCtx, server, settings.VerboseLogging)
		cancel()
		if err != nil {
			if settings.VerboseLogging {
				logMCP.Warnf("discover.failed server=%s transport=%s err=%v", server.ID, server.Transport, err)
			}
			warnings = append(warnings, fmt.Sprintf("mcp server %s discovery failed: %v", server.ID, err))
			m.updateStatus(server.ID, func(status *ServerStatus) {
				status.Enabled = true
				status.Transport = server.Transport
				status.Reachable = false
				status.LastError = truncateMCPError(err.Error(), 240)
				status.RegisteredTools = nil
				status.DiscoveredTools = nil
				status.UpdatedAt = time.Now().UTC()
			})
			continue
		}
		if settings.VerboseLogging {
			logMCP.Logf("discover.done server=%s discovered=%d", server.ID, len(discovered))
		}

		allowlist := make(map[string]struct{}, len(server.ToolAllowlist))
		for _, name := range server.ToolAllowlist {
			allowlist[strings.TrimSpace(name)] = struct{}{}
		}
		autoRegisterAll := shouldAutoRegisterAllDiscoveredTools(server.ID)
		if settings.VerboseLogging {
			logMCP.Logf(
				"discover.register server=%s auto_all=%t allowlist=%d discovered=%d",
				server.ID,
				autoRegisterAll,
				len(allowlist),
				len(discovered),
			)
		}

		registered := make([]string, 0, len(discovered))
		discoveredNames := make([]string, 0, len(discovered))
		for _, descriptor := range discovered {
			discoveredNames = append(discoveredNames, descriptor.Name)
			if !autoRegisterAll {
				if _, ok := allowlist[descriptor.Name]; !ok {
					continue
				}
			}
			exposedName := ExposedToolName(server.ID, descriptor.Name)
			tools = append(tools, NewToolAdapter(m, server.ID, descriptor.Name, exposedName, descriptor.Description, descriptor.InputSchema))
			registered = append(registered, exposedName)
		}

		if settings.VerboseLogging {
			logMCP.Logf(
				"discover.register.done server=%s registered=%d auto_all=%t",
				server.ID,
				len(registered),
				autoRegisterAll,
			)
		}

		// Collect per-server instructions for system prompt injection.
		if instr := strings.TrimSpace(server.Instructions); instr != "" && len(registered) > 0 {
			label := strings.TrimSpace(server.Name)
			if label == "" {
				label = server.ID
			}
			m.mu.Lock()
			m.mcpInstructions = append(m.mcpInstructions, fmt.Sprintf("[%s]\n%s", label, instr))
			m.mu.Unlock()
		}

		m.updateStatus(server.ID, func(status *ServerStatus) {
			status.Enabled = true
			status.Transport = server.Transport
			status.Reachable = true
			status.LastError = ""
			status.DiscoveredTools = sortStrings(discoveredNames)
			status.RegisteredTools = sortStrings(registered)
			status.UpdatedAt = time.Now().UTC()
		})
	}

	return tools, warnings
}

func shouldAutoRegisterAllDiscoveredTools(serverID string) bool {
	return strings.TrimSpace(serverID) == config.ManagedPlaywrightMCPServerID
}

func (m *Manager) DiscoverServer(ctx context.Context, server config.MCPServerSetting, timeout time.Duration) ([]ToolDescriptor, error) {
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultMCPDiscoveryTimeoutSeconds) * time.Second
	}

	timedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tools, err := m.discoverWithServer(timedCtx, server, m.verboseLoggingEnabled())
	if m.verboseLoggingEnabled() {
		if err != nil {
			logMCP.Warnf("discover.manual.failed server=%s transport=%s err=%v", server.ID, server.Transport, err)
		} else {
			logMCP.Logf("discover.manual.done server=%s discovered=%d", server.ID, len(tools))
		}
	}
	if server.ID != "" {
		if err != nil {
			m.updateStatus(server.ID, func(status *ServerStatus) {
				status.ServerID = server.ID
				status.Enabled = server.Enabled
				status.Transport = server.Transport
				status.Reachable = false
				status.LastError = truncateMCPError(err.Error(), 240)
				status.UpdatedAt = time.Now().UTC()
			})
		} else {
			names := make([]string, 0, len(tools))
			for _, tool := range tools {
				names = append(names, tool.Name)
			}
			m.updateStatus(server.ID, func(status *ServerStatus) {
				status.ServerID = server.ID
				status.Enabled = server.Enabled
				status.Transport = server.Transport
				status.Reachable = true
				status.LastError = ""
				status.DiscoveredTools = sortStrings(names)
				status.UpdatedAt = time.Now().UTC()
			})
		}
	}
	return tools, err
}

func (m *Manager) CallTool(ctx context.Context, serverID, toolName string, input map[string]interface{}) (string, bool, error) {
	server, timeout, verbose, err := m.lookupServer(serverID)
	if err != nil {
		return "", false, err
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if verbose {
		logMCP.Logf(
			"tool.call.start server=%s transport=%s tool=%s timeout=%s input=%s",
			serverID,
			server.Transport,
			toolName,
			timeout,
			string(sanitizeJSONBytes(mustMarshalJSON(input))),
		)
	}

	var content string
	var isError bool
	switch strings.TrimSpace(server.Transport) {
	case TransportStdio:
		content, isError, err = runStdioCallTool(callCtx, server, toolName, input, verbose)
	case TransportHTTP:
		content, isError, err = runHTTPCallTool(callCtx, m.httpClient, server, toolName, input, verbose)
	case TransportSSE:
		content, isError, err = runSSECallTool(callCtx, m.httpClient, server, toolName, input, verbose)
	default:
		err = fmt.Errorf("unsupported MCP transport: %s", server.Transport)
	}

	if err != nil {
		m.updateStatus(serverID, func(status *ServerStatus) {
			status.Reachable = false
			status.LastError = truncateMCPError(err.Error(), 240)
			status.UpdatedAt = time.Now().UTC()
		})
		return "", false, err
	}
	if verbose {
		logMCP.Logf(
			"tool.call.done server=%s tool=%s is_error=%t output=%s",
			serverID,
			toolName,
			isError,
			sanitizePayloadText(content),
		)
	}

	m.updateStatus(serverID, func(status *ServerStatus) {
		status.Reachable = true
		status.LastError = ""
		status.UpdatedAt = time.Now().UTC()
	})
	return content, isError, nil
}

// ServerInstructions returns per-server usage guidelines collected during
// the most recent BuildTools call. Each entry is formatted as "[ServerName]\n<instructions>".
func (m *Manager) ServerInstructions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.mcpInstructions) == 0 {
		return nil
	}
	out := make([]string, len(m.mcpInstructions))
	copy(out, m.mcpInstructions)
	return out
}

func (m *Manager) StatusSnapshot() []ServerStatus {
	m.mu.RLock()
	statuses := make([]ServerStatus, 0, len(m.statuses))
	for _, status := range m.statuses {
		statuses = append(statuses, status)
	}
	m.mu.RUnlock()

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ServerID < statuses[j].ServerID
	})
	return statuses
}

func (m *Manager) lookupServer(serverID string) (config.MCPServerSetting, time.Duration, bool, error) {
	m.mu.RLock()
	server, ok := m.servers[serverID]
	settings := m.settings
	m.mu.RUnlock()
	if !ok {
		return config.MCPServerSetting{}, 0, false, fmt.Errorf("MCP server %q not found", serverID)
	}
	if !server.Enabled {
		return config.MCPServerSetting{}, 0, false, fmt.Errorf("MCP server %q is disabled", serverID)
	}
	timeout := time.Duration(settings.ToolCallTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultMCPToolCallTimeoutSeconds) * time.Second
	}
	return server, timeout, settings.VerboseLogging, nil
}

func (m *Manager) discoverWithServer(ctx context.Context, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	if verbose {
		logMCP.Logf("discover.start server=%s transport=%s", server.ID, server.Transport)
	}
	switch strings.TrimSpace(server.Transport) {
	case TransportStdio:
		return runStdioListTools(ctx, server, verbose)
	case TransportHTTP:
		return runHTTPListTools(ctx, m.httpClient, server, verbose)
	case TransportSSE:
		return runSSEListTools(ctx, m.httpClient, server, verbose)
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", server.Transport)
	}
}

func (m *Manager) resetRegistrations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, status := range m.statuses {
		status.Reachable = false
		status.RegisteredTools = nil
		status.UpdatedAt = time.Now().UTC()
		m.statuses[key] = status
	}
}

func (m *Manager) updateStatus(serverID string, update func(status *ServerStatus)) {
	if strings.TrimSpace(serverID) == "" {
		return
	}
	m.mu.Lock()
	status, ok := m.statuses[serverID]
	if !ok {
		status = ServerStatus{ServerID: serverID}
	}
	update(&status)
	m.statuses[serverID] = status
	m.mu.Unlock()
}

func truncateMCPError(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen || maxLen <= 3 {
		return text
	}
	return text[:maxLen-3] + "..."
}

func (m *Manager) verboseLoggingEnabled() bool {
	m.mu.RLock()
	enabled := m.settings.VerboseLogging
	m.mu.RUnlock()
	return enabled
}

func mustMarshalJSON(value any) []byte {
	if value == nil {
		return []byte("null")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return []byte(fmt.Sprintf("%v", value))
	}
	return data
}
