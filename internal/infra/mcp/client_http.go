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
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

type httpSession struct {
	client    *http.Client
	server    config.MCPServerSetting
	sessionID string
	nextID    int64
	verbose   bool
}

func newHTTPSession(client *http.Client, server config.MCPServerSetting, verbose bool) *httpSession {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpSession{
		client:  client,
		server:  server,
		verbose: verbose,
	}
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

	s.logRequest(method, request.ID, httpReq.Header, body)
	startedAt := time.Now()
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		s.logResponse(method, request.ID, 0, "", time.Since(startedAt), nil, err)
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if sid := strings.TrimSpace(httpResp.Header.Get("Mcp-Session-Id")); sid != "" {
		s.sessionID = sid
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		s.logResponse(method, request.ID, httpResp.StatusCode, httpResp.Header.Get("Content-Type"), time.Since(startedAt), nil, err)
		return nil, fmt.Errorf("read response body: %w", err)
	}
	s.logResponse(method, request.ID, httpResp.StatusCode, httpResp.Header.Get("Content-Type"), time.Since(startedAt), respBody, nil)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if !expectResponse {
		return nil, nil
	}

	payload, err := extractHTTPResponsePayload(httpResp.Header.Get("Content-Type"), respBody, request.ID)
	if err != nil {
		return nil, err
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

func extractHTTPResponsePayload(contentType string, body []byte, requestID interface{}) ([]byte, error) {
	payload := bytes.TrimSpace(body)
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty response payload")
	}
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		selected, err := selectSSEDataPayloadForRequest(payload, requestID)
		if err != nil {
			return nil, err
		}
		return bytes.TrimSpace(selected), nil
	}
	return payload, nil
}

func (s *httpSession) logRequest(rpcMethod string, requestID interface{}, headers http.Header, body []byte) {
	if !s.verbose {
		return
	}
	logMCP.Logf(
		"http.req server=%s transport=http rpc_method=%s rpc_id=%v session=%s headers=%v payload=%s",
		s.server.ID,
		rpcMethod,
		requestID,
		s.sessionID,
		sanitizeHeaderValues(headers),
		string(sanitizeJSONBytes(body)),
	)
}

func (s *httpSession) logResponse(rpcMethod string, requestID interface{}, status int, contentType string, elapsed time.Duration, body []byte, callErr error) {
	if !s.verbose {
		return
	}
	if callErr != nil {
		logMCP.Warnf(
			"http.res server=%s transport=http rpc_method=%s rpc_id=%v session=%s status=%d elapsed_ms=%d err=%v",
			s.server.ID,
			rpcMethod,
			requestID,
			s.sessionID,
			status,
			elapsed.Milliseconds(),
			callErr,
		)
		return
	}

	logMCP.Logf(
		"http.res server=%s transport=http rpc_method=%s rpc_id=%v session=%s status=%d content_type=%q elapsed_ms=%d payload=%s",
		s.server.ID,
		rpcMethod,
		requestID,
		s.sessionID,
		status,
		contentType,
		elapsed.Milliseconds(),
		sanitizeHTTPResponseBody(contentType, body),
	)
}

func sanitizeHTTPResponseBody(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		events := parseSSEEvents(body)
		if len(events) == 0 {
			return string(body)
		}
		var b strings.Builder
		for idx, event := range events {
			if idx > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("event=")
			if strings.TrimSpace(event.Event) == "" {
				b.WriteString("message")
			} else {
				b.WriteString(event.Event)
			}
			b.WriteString(" data=")
			b.WriteString(sanitizePayloadText(string(event.Data)))
		}
		return b.String()
	}
	return string(sanitizeJSONBytes(body))
}

func runHTTPListTools(ctx context.Context, client *http.Client, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	session := newHTTPSession(client, server, verbose)
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

func runHTTPCallTool(
	ctx context.Context,
	client *http.Client,
	server config.MCPServerSetting,
	toolName string,
	input map[string]interface{},
	verbose bool,
) (string, bool, error) {
	session := newHTTPSession(client, server, verbose)
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
