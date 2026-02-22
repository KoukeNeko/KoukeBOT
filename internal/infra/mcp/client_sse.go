package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

// sseSession implements the Legacy SSE transport (MCP spec pre-2025-03-26).
//
// Protocol flow:
//  1. GET server.URL → SSE stream with "event: endpoint" containing the message URL
//  2. POST JSON-RPC requests to the discovered message URL
type sseSession struct {
	client     *http.Client
	server     config.MCPServerSetting
	messageURL string // discovered via SSE handshake
	nextID     int64
	verbose    bool
}

func newSSESession(client *http.Client, server config.MCPServerSetting, verbose bool) *sseSession {
	if client == nil {
		client = http.DefaultClient
	}
	return &sseSession{
		client:  client,
		server:  server,
		verbose: verbose,
	}
}

// handshake performs a GET to server.URL, reads the SSE stream until an
// "endpoint" event is found, resolves the relative path into a full URL,
// and stores the result in s.messageURL.
func (s *sseSession) handshake(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL, nil)
	if err != nil {
		return fmt.Errorf("build SSE handshake request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for key, value := range s.server.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	if s.verbose {
		logMCP.Logf("sse.handshake server=%s url=%s", s.server.ID, s.server.URL)
	}

	startedAt := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		if s.verbose {
			logMCP.Warnf("sse.handshake.failed server=%s elapsed_ms=%d err=%v", s.server.ID, time.Since(startedAt).Milliseconds(), err)
		}
		return fmt.Errorf("SSE handshake request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("SSE handshake status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Scan line-by-line for the "endpoint" event.
	// The server keeps this connection open, so we read incrementally
	// and return as soon as the endpoint is found.
	scanner := bufio.NewScanner(resp.Body)
	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if currentEvent == "endpoint" && data != "" {
				resolved, err := resolveEndpointURL(s.server.URL, data)
				if err != nil {
					return err
				}
				s.messageURL = resolved
				if s.verbose {
					logMCP.Logf("sse.handshake.done server=%s message_url=%s elapsed_ms=%d", s.server.ID, s.messageURL, time.Since(startedAt).Milliseconds())
				}
				return nil
			}
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE handshake stream read error: %w", err)
	}
	return fmt.Errorf("SSE handshake: no endpoint event received from %s", s.server.URL)
}

// resolveEndpointURL resolves a (possibly relative) endpoint path against
// the base SSE URL.
func resolveEndpointURL(baseURL, endpointPath string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}
	ref, err := url.Parse(endpointPath)
	if err != nil {
		return "", fmt.Errorf("parse endpoint path %q: %w", endpointPath, err)
	}
	return base.ResolveReference(ref).String(), nil
}

// call posts a JSON-RPC request to the discovered message URL and returns
// the result. Must be called after a successful handshake.
func (s *sseSession) call(ctx context.Context, method string, params interface{}, expectResponse bool) (json.RawMessage, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.messageURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
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

func (s *sseSession) logRequest(rpcMethod string, requestID interface{}, headers http.Header, body []byte) {
	if !s.verbose {
		return
	}
	logMCP.Logf(
		"sse.req server=%s transport=sse rpc_method=%s rpc_id=%v message_url=%s headers=%v payload=%s",
		s.server.ID,
		rpcMethod,
		requestID,
		s.messageURL,
		sanitizeHeaderValues(headers),
		string(sanitizeJSONBytes(body)),
	)
}

func (s *sseSession) logResponse(rpcMethod string, requestID interface{}, status int, contentType string, elapsed time.Duration, body []byte, callErr error) {
	if !s.verbose {
		return
	}
	if callErr != nil {
		logMCP.Warnf(
			"sse.res server=%s transport=sse rpc_method=%s rpc_id=%v message_url=%s status=%d elapsed_ms=%d err=%v",
			s.server.ID,
			rpcMethod,
			requestID,
			s.messageURL,
			status,
			elapsed.Milliseconds(),
			callErr,
		)
		return
	}

	logMCP.Logf(
		"sse.res server=%s transport=sse rpc_method=%s rpc_id=%v message_url=%s status=%d content_type=%q elapsed_ms=%d payload=%s",
		s.server.ID,
		rpcMethod,
		requestID,
		s.messageURL,
		status,
		contentType,
		elapsed.Milliseconds(),
		sanitizeHTTPResponseBody(contentType, body),
	)
}

func runSSEListTools(ctx context.Context, client *http.Client, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	session := newSSESession(client, server, verbose)
	if err := session.handshake(ctx); err != nil {
		return nil, fmt.Errorf("SSE handshake failed: %w", err)
	}
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

func runSSECallTool(
	ctx context.Context,
	client *http.Client,
	server config.MCPServerSetting,
	toolName string,
	input map[string]interface{},
	verbose bool,
) (string, bool, error) {
	session := newSSESession(client, server, verbose)
	if err := session.handshake(ctx); err != nil {
		return "", false, fmt.Errorf("SSE handshake failed: %w", err)
	}
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
