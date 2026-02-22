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
	"sync"
	"sync/atomic"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/config"
)

// sseSession implements the Legacy SSE transport (MCP spec pre-2025-03-26).
//
// Protocol flow:
//  1. GET server.URL → persistent SSE stream; first event is "endpoint"
//  2. POST JSON-RPC requests to the discovered message URL (server responds 202)
//  3. JSON-RPC responses arrive as "message" events on the SSE stream
type sseSession struct {
	client     *http.Client
	server     config.MCPServerSetting
	messageURL string // discovered via SSE handshake
	nextID     int64
	verbose    bool

	// SSE stream state — kept open for the session lifetime
	streamResp *http.Response  // the GET /sse response; closed in close()
	incoming   chan sseMessage // events parsed from the stream
	streamErr  error           // terminal error from the reader goroutine
	streamOnce sync.Once       // ensures close() is idempotent
}

// sseMessage carries a parsed SSE event from the background reader.
type sseMessage struct {
	Event string
	Data  []byte
}

func newSSESession(client *http.Client, server config.MCPServerSetting, verbose bool) *sseSession {
	if client == nil {
		client = http.DefaultClient
	}
	return &sseSession{
		client:   client,
		server:   server,
		verbose:  verbose,
		incoming: make(chan sseMessage, 32),
	}
}

// connect performs a GET to server.URL, waits for the "endpoint" event,
// stores the message URL, and starts a background goroutine that feeds
// subsequent SSE events into s.incoming.
func (s *sseSession) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL, nil)
	if err != nil {
		return fmt.Errorf("build SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for key, value := range s.server.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	if s.verbose {
		logMCP.Logf("sse.connect server=%s url=%s", s.server.ID, s.server.URL)
	}

	startedAt := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		if s.verbose {
			logMCP.Warnf("sse.connect.failed server=%s elapsed_ms=%d err=%v", s.server.ID, time.Since(startedAt).Milliseconds(), err)
		}
		return fmt.Errorf("SSE connect failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return fmt.Errorf("SSE connect status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	s.streamResp = resp

	// Read lines until we find the endpoint event.
	scanner := bufio.NewScanner(resp.Body)
	endpointFound, err := s.scanForEndpoint(scanner)
	if err != nil {
		resp.Body.Close()
		return fmt.Errorf("SSE stream read error: %w", err)
	}
	if !endpointFound {
		resp.Body.Close()
		return fmt.Errorf("SSE connect: no endpoint event received from %s", s.server.URL)
	}

	if s.verbose {
		logMCP.Logf("sse.connect.done server=%s message_url=%s elapsed_ms=%d", s.server.ID, s.messageURL, time.Since(startedAt).Milliseconds())
	}

	// Start background reader for subsequent events.
	go s.readStream(scanner)

	return nil
}

// scanForEndpoint reads SSE lines until the "endpoint" event is found.
// Returns true if found, false if the stream ended without it.
func (s *sseSession) scanForEndpoint(scanner *bufio.Scanner) (bool, error) {
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
					return false, err
				}
				s.messageURL = resolved
				return true, nil
			}
		}
	}
	return false, scanner.Err()
}

// readStream runs in a goroutine, parsing SSE events and pushing them
// into s.incoming. It closes the channel when the stream ends.
func (s *sseSession) readStream(scanner *bufio.Scanner) {
	defer close(s.incoming)

	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = event boundary; reset for next event.
			currentEvent = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment (heartbeat); skip.
			continue
		}
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				s.incoming <- sseMessage{Event: currentEvent, Data: []byte(data)}
			}
		}
	}
	s.streamErr = scanner.Err()
}

// close shuts down the SSE stream.
func (s *sseSession) close() {
	s.streamOnce.Do(func() {
		if s.streamResp != nil {
			s.streamResp.Body.Close()
		}
	})
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

// call posts a JSON-RPC request to the message URL and reads the response
// from the SSE stream. Must be called after a successful connect().
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

	// POST the request to the message URL.
	postErr := s.postMessage(ctx, method, request.ID, body)
	if postErr != nil {
		return nil, postErr
	}

	if !expectResponse {
		return nil, nil
	}

	// Wait for the matching response on the SSE stream.
	return s.waitForResponse(ctx, id, method)
}

// postMessage sends a JSON-RPC request to the message URL via HTTP POST.
func (s *sseSession) postMessage(ctx context.Context, rpcMethod string, requestID interface{}, body []byte) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.messageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range s.server.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}

	s.logRequest(rpcMethod, requestID, httpReq.Header, body)
	startedAt := time.Now()
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		s.logPostResponse(rpcMethod, requestID, 0, time.Since(startedAt), err)
		return fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()
	// Drain body to allow connection reuse.
	_, _ = io.Copy(io.Discard, httpResp.Body)

	s.logPostResponse(rpcMethod, requestID, httpResp.StatusCode, time.Since(startedAt), nil)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("http status %d posting %s", httpResp.StatusCode, rpcMethod)
	}
	return nil
}

// waitForResponse reads SSE events until a JSON-RPC response matching the
// given request ID arrives, or the context is cancelled.
func (s *sseSession) waitForResponse(ctx context.Context, requestID int64, rpcMethod string) (json.RawMessage, error) {
	startedAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for SSE response to %s (id=%d): %w", rpcMethod, requestID, ctx.Err())

		case msg, ok := <-s.incoming:
			if !ok {
				// Stream closed.
				if s.streamErr != nil {
					return nil, fmt.Errorf("SSE stream closed with error: %w", s.streamErr)
				}
				return nil, fmt.Errorf("SSE stream closed before receiving response to %s (id=%d)", rpcMethod, requestID)
			}

			// Try to parse as JSON-RPC response.
			var resp rpcResponse
			if err := json.Unmarshal(msg.Data, &resp); err != nil {
				// Not a valid JSON-RPC response; skip (could be a notification).
				continue
			}

			// Match by request ID.
			if fmt.Sprint(resp.ID) != fmt.Sprint(requestID) {
				continue
			}

			s.logStreamResponse(rpcMethod, requestID, time.Since(startedAt), msg.Data)

			if resp.Error != nil {
				return nil, fmt.Errorf("%s", rpcErrorMessage(resp))
			}
			if len(resp.Result) > 0 {
				return resp.Result, nil
			}
			return nil, nil
		}
	}
}

// --- Logging helpers ---

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

func (s *sseSession) logPostResponse(rpcMethod string, requestID interface{}, status int, elapsed time.Duration, callErr error) {
	if !s.verbose {
		return
	}
	if callErr != nil {
		logMCP.Warnf(
			"sse.post server=%s transport=sse rpc_method=%s rpc_id=%v status=%d elapsed_ms=%d err=%v",
			s.server.ID, rpcMethod, requestID, status, elapsed.Milliseconds(), callErr,
		)
		return
	}
	logMCP.Logf(
		"sse.post server=%s transport=sse rpc_method=%s rpc_id=%v status=%d elapsed_ms=%d",
		s.server.ID, rpcMethod, requestID, status, elapsed.Milliseconds(),
	)
}

func (s *sseSession) logStreamResponse(rpcMethod string, requestID interface{}, elapsed time.Duration, data []byte) {
	if !s.verbose {
		return
	}
	logMCP.Logf(
		"sse.stream server=%s transport=sse rpc_method=%s rpc_id=%v elapsed_ms=%d payload=%s",
		s.server.ID, rpcMethod, requestID, elapsed.Milliseconds(), string(sanitizeJSONBytes(data)),
	)
}

// --- Top-level orchestration ---

func runSSEListTools(ctx context.Context, client *http.Client, server config.MCPServerSetting, verbose bool) ([]ToolDescriptor, error) {
	session := newSSESession(client, server, verbose)
	if err := session.connect(ctx); err != nil {
		return nil, fmt.Errorf("SSE connect failed: %w", err)
	}
	defer session.close()

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
	if err := session.connect(ctx); err != nil {
		return "", false, fmt.Errorf("SSE connect failed: %w", err)
	}
	defer session.close()

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
