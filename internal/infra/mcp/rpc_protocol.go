package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func newInitializeParams() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "koukebot",
			"version": "0.1.0",
		},
	}
}

func parseToolsListResult(raw json.RawMessage) ([]ToolDescriptor, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty tools/list response")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode tools/list response: %w", err)
	}

	toolsValue, ok := payload["tools"]
	if !ok {
		return nil, errors.New("tools/list response missing tools")
	}

	items, ok := toolsValue.([]interface{})
	if !ok {
		return nil, errors.New("tools/list tools should be array")
	}

	tools := make([]ToolDescriptor, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		description, _ := entry["description"].(string)
		description = strings.TrimSpace(description)

		inputSchema := map[string]interface{}{
			"type": "object",
		}
		if schema, ok := entry["inputSchema"].(map[string]interface{}); ok && len(schema) > 0 {
			inputSchema = schema
		} else if schema, ok := entry["input_schema"].(map[string]interface{}); ok && len(schema) > 0 {
			inputSchema = schema
		}

		tools = append(tools, ToolDescriptor{
			Name:        name,
			Description: description,
			InputSchema: inputSchema,
		})
	}

	return tools, nil
}

func parseToolCallResponse(raw json.RawMessage) (content string, isError bool, err error) {
	if len(raw) == 0 {
		return "", false, errors.New("empty tools/call response")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return string(raw), false, nil
	}

	if v, ok := payload["isError"].(bool); ok {
		isError = v
	}

	if contentValue, ok := payload["content"]; ok {
		if rendered := renderMCPContent(contentValue); strings.TrimSpace(rendered) != "" {
			return rendered, isError, nil
		}
	}

	if structured, ok := payload["structuredContent"]; ok {
		if b, err := json.MarshalIndent(structured, "", "  "); err == nil {
			return string(b), isError, nil
		}
	}

	if b, err := json.MarshalIndent(payload, "", "  "); err == nil {
		return string(b), isError, nil
	}

	return string(raw), isError, nil
}

func renderMCPContent(value interface{}) string {
	items, ok := value.([]interface{})
	if !ok {
		if b, err := json.MarshalIndent(value, "", "  "); err == nil {
			return string(b)
		}
		return ""
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := entry["text"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
			continue
		}
		if b, err := json.MarshalIndent(entry, "", "  "); err == nil {
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}

func rpcErrorMessage(resp rpcResponse) string {
	if resp.Error == nil {
		return ""
	}
	if resp.Error.Data == nil {
		return fmt.Sprintf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	data, _ := json.Marshal(resp.Error.Data)
	return fmt.Sprintf("RPC error %d: %s (%s)", resp.Error.Code, resp.Error.Message, string(data))
}
