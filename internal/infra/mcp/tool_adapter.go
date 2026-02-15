package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

type ToolAdapter struct {
	manager     *Manager
	serverID    string
	remoteName  string
	exposedName string
	description string
	schema      map[string]interface{}
}

func NewToolAdapter(manager *Manager, serverID, remoteName, exposedName, description string, schema map[string]interface{}) *ToolAdapter {
	if len(schema) == 0 {
		schema = map[string]interface{}{"type": "object"}
	}
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", remoteName, serverID)
	}
	return &ToolAdapter{
		manager:     manager,
		serverID:    serverID,
		remoteName:  remoteName,
		exposedName: exposedName,
		description: desc,
		schema:      schema,
	}
}

func (t *ToolAdapter) Name() string {
	return t.exposedName
}

func (t *ToolAdapter) Description() string {
	return t.description
}

func (t *ToolAdapter) InputSchema() map[string]interface{} {
	return t.schema
}

func (t *ToolAdapter) Execute(ctx context.Context, input map[string]interface{}) (domain.ToolResult, error) {
	if t.manager == nil {
		return domain.ToolResult{}, fmt.Errorf("MCP manager is not configured")
	}
	content, isError, err := t.manager.CallTool(ctx, t.serverID, t.remoteName, input)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return domain.ToolResult{Content: content, IsError: isError}, nil
}
