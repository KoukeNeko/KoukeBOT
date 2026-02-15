package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

type httpSession struct {
	client    *http.Client
	server    config.MCPServerSetting
	sessionID string
	nextID    int64
}

func newHTTPSession(client *http.Client, server config.MCPServerSetting) *httpSession {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpSession{client: client, server: server}
}

func (s *httpSession) call(ctx context.Context, method string, params interface{}, expectResponse bool) (json.RawMessage, error) {
	id := atomic.AddInt64(&s.nextID, 1)
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if !expectResponse {
		request.ID = nil
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.server.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if s.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	for key, value := range s.server.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if sid := strings.TrimSpace(httpResp.Header.Get("Mcp-Session-Id")); sid != "" {
		s.sessionID = sid
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if !expectResponse {
		return nil, nil
	}

	payload := bytes.TrimSpace(respBody)
	if strings.Contains(strings.ToLower(httpResp.Header.Get("Content-Type")), "text/event-stream") {
		if data := parseSSEDataPayload(payload); len(data) > 0 {
			payload = data
		}
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty response payload")
	}

	var response rpcResponse
	if err := json.Unmarshal(payload, &response); err == nil && (response.Error != nil || len(response.Result) > 0 || response.JSONRPC != "") {
		if response.Error != nil {
			return nil, fmt.Errorf("%s", rpcErrorMessage(response))
		}
		if len(response.Result) > 0 {
			return response.Result, nil
		}
		return nil, nil
	}

	return payload, nil
}

func runHTTPListTools(ctx context.Context, client *http.Client, server config.MCPServerSetting) ([]ToolDescriptor, error) {
	session := newHTTPSession(client, server)
	if _, err := session.call(ctx, "initialize", newInitializeParams(), true); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}
	_, _ = session.call(ctx, "notifications/initialized", map[string]interface{}{}, false)

	result, err := session.call(ctx, "tools/list", map[string]interface{}{}, true)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}
	tools, err := parseToolsListResult(result)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func runHTTPCallTool(ctx context.Context, client *http.Client, server config.MCPServerSetting, toolName string, input map[string]interface{}) (string, bool, error) {
	session := newHTTPSession(client, server)
	if _, err := session.call(ctx, "initialize", newInitializeParams(), true); err != nil {
		return "", false, fmt.Errorf("initialize failed: %w", err)
	}
	_, _ = session.call(ctx, "notifications/initialized", map[string]interface{}{}, false)

	result, err := session.call(ctx, "tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": input,
	}, true)
	if err != nil {
		return "", false, fmt.Errorf("tools/call failed: %w", err)
	}

	content, isError, err := parseToolCallResponse(result)
	if err != nil {
		return "", false, err
	}
	return content, isError, nil
}
