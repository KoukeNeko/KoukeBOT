package llm

// gemini_oauth_stream.go — SSE stream consumption and the streaming Provider
// methods (Chat, StreamChat, StreamChatMultimodal) for GeminiOAuthProvider.
//
// Separation rationale: all code that reads the SSE wire format lives here,
// making it easy to change the streaming protocol independently of HTTP
// transport (gemini_oauth_http.go) and schema conversion (gemini_oauth_schema.go).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
)

// Empty stream retry constants (aligned with pi-ai).
const (
	maxEmptyStreamRetries = 2
	emptyStreamBaseDelay  = 500 * time.Millisecond
)

func (g *GeminiOAuthProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Retry loop for empty streams (pi-ai: MAX_EMPTY_STREAM_RETRIES = 2).
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist request")
		if err != nil {
			return nil, err
		}

		result, emptyStream := g.consumeSSEResponse(resp)
		if !emptyStream {
			return result.chatResponse, result.err
		}

		resp.Body.Close()
		if emptyRetry < maxEmptyStreamRetries {
			delay := emptyStreamBaseDelay * time.Duration(1<<emptyRetry)
			logOAuth.Warnf("empty stream (attempt %d/%d), retrying in %s",
				emptyRetry+1, maxEmptyStreamRetries+1, delay)
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
	}

	return nil, fmt.Errorf("empty stream: server returned 200 OK but no content after %d retries", maxEmptyStreamRetries+1)
}

// chatResult holds the result of consuming an SSE response for Chat().
type chatResult struct {
	chatResponse *ChatResponse
	err          error
}

// consumeSSEResponse reads an SSE stream and returns the parsed result.
// Returns (result, true) if the stream was empty (should retry).
func (g *GeminiOAuthProvider) consumeSSEResponse(resp *http.Response) (chatResult, bool) {
	defer resp.Body.Close()

	var fullText strings.Builder
	var totalTokens int
	var lastFinishReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		data, ok := parseSSEData(scanner.Text())
		if !ok {
			continue
		}
		var chunk caResponseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Response != nil && len(chunk.Response.Candidates) > 0 {
			if fr := chunk.Response.Candidates[0].FinishReason; fr != "" {
				lastFinishReason = fr
			}
		}
		parsed := extractCAChunkFull(chunk)
		fullText.WriteString(parsed.text)
		if parsed.tokens > 0 {
			totalTokens = parsed.tokens
		}
	}

	if err := scanner.Err(); err != nil {
		return chatResult{err: fmt.Errorf("read SSE stream: %w", err)}, false
	}

	finalContent := fullText.String()

	// Empty stream detection — signal caller to retry.
	if finalContent == "" && totalTokens == 0 && lastFinishReason == "" {
		return chatResult{}, true
	}

	switch lastFinishReason {
	case "MAX_TOKENS":
		logOAuth.Warnf("response truncated (MAX_TOKENS) — consider increasing max_tokens")
	case "SAFETY":
		return chatResult{err: fmt.Errorf("response blocked by safety filters")}, false
	case "RECITATION":
		return chatResult{err: fmt.Errorf("response blocked due to recitation concerns")}, false
	case "BLOCKLIST":
		return chatResult{err: fmt.Errorf("response blocked by content blocklist")}, false
	case "", "STOP":
		// Normal completion
	default:
		logOAuth.Warnf("unexpected finish reason: %s", lastFinishReason)
	}

	return chatResult{chatResponse: &ChatResponse{
		Content:    finalContent,
		TokensUsed: totalTokens,
	}}, false
}

func (g *GeminiOAuthProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(g.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Retry empty streams at HTTP level before handing off to the streaming goroutine.
	var resp *http.Response
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		var openErr error
		resp, _, openErr = g.openStreamGenerateContent(ctx, body, "code assist stream")
		if openErr != nil {
			return nil, openErr
		}

		// Peek: try to read until we get the first data line or EOF.
		// If we can confirm it's a non-empty stream, break and stream it.
		// For streaming we can't fully consume, so we stream directly and
		// handle empty stream in the goroutine with retry signaling.
		break
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		g.streamSSEWithEmptyRetry(ctx, body, resp, ch)
	}()

	return ch, nil
}

// streamSSEWithEmptyRetry reads an SSE stream and retries on empty streams.
func (g *GeminiOAuthProvider) streamSSEWithEmptyRetry(ctx context.Context, body []byte, resp *http.Response, ch chan<- StreamChunk) {
	for emptyRetry := 0; emptyRetry <= maxEmptyStreamRetries; emptyRetry++ {
		emptyStream := g.consumeStreamSSE(resp, ch)
		resp.Body.Close()

		if !emptyStream {
			return // Normal completion or error already sent to channel
		}

		// Empty stream — retry if we have attempts left.
		if emptyRetry >= maxEmptyStreamRetries {
			ch <- StreamChunk{
				Error: fmt.Errorf("empty stream: server returned 200 OK but no content after %d retries", maxEmptyStreamRetries+1),
				Done:  true,
			}
			return
		}

		delay := emptyStreamBaseDelay * time.Duration(1<<emptyRetry)
		logOAuth.Warnf("empty stream (attempt %d/%d), retrying in %s",
			emptyRetry+1, maxEmptyStreamRetries+1, delay)
		if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
			ch <- StreamChunk{Error: sleepErr, Done: true}
			return
		}

		// Re-open the stream for retry.
		var openErr error
		resp, _, openErr = g.openStreamGenerateContent(ctx, body, "code assist stream")
		if openErr != nil {
			ch <- StreamChunk{Error: openErr, Done: true}
			return
		}
	}
}

// consumeStreamSSE reads an SSE stream into the channel.
// Returns true if the stream was empty (caller should retry).
func (g *GeminiOAuthProvider) consumeStreamSSE(resp *http.Response, ch chan<- StreamChunk) bool {
	var lastUsage *caUsage
	var lastFinishReason string
	var responseLen int
	var responsePreview string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		data, ok := parseSSEData(scanner.Text())
		if !ok {
			continue
		}
		var chunk caResponseChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		parsed := extractCAChunkFull(chunk)
		if parsed.usage != nil {
			lastUsage = parsed.usage
		}
		if parsed.finishReason != "" {
			lastFinishReason = parsed.finishReason
		}

		// Emit function calls to trigger the tool calling loop
		if len(parsed.toolCalls) > 0 {
			logOAuth.Logf("functionCall: %s", formatToolCallNames(parsed.toolCalls))
			logUsage(lastUsage)
			tokens := 0
			if lastUsage != nil {
				tokens = lastUsage.TotalTokenCount
			}
			ch <- StreamChunk{ToolCalls: parsed.toolCalls, Done: true, TokensUsed: tokens}
			return false
		}

		if parsed.text != "" {
			responseLen += len(parsed.text)
			if len(responsePreview) < 80 {
				responsePreview += parsed.text
			}
			ch <- StreamChunk{Content: parsed.text}
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Error: err, Done: true}
		return false
	}

	// Empty stream: signal caller to retry.
	if responseLen == 0 && lastFinishReason == "" {
		return true
	}

	logCompletion(lastUsage, responsePreview, responseLen)
	if lastFinishReason == "MAX_TOKENS" {
		logOAuth.Warnf("stream response truncated (MAX_TOKENS) — consider increasing max_tokens")
	}
	tokens := 0
	if lastUsage != nil {
		tokens = lastUsage.TotalTokenCount
	}
	ch <- StreamChunk{Done: true, TokensUsed: tokens, FinishReason: lastFinishReason}
	return false
}

func (g *GeminiOAuthProvider) StreamChatMultimodal(ctx context.Context, systemPrompt string, messages []domain.MultimodalMessage, generation domain.GenerationConfig) (<-chan StreamChunk, error) {
	body, err := json.Marshal(g.buildMultimodalRequest(systemPrompt, messages, generation))
	if err != nil {
		return nil, fmt.Errorf("marshal multimodal request: %w", err)
	}

	resp, _, err := g.openStreamGenerateContent(ctx, body, "code assist multimodal stream")
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var lastUsage *caUsage
		var responseLen int
		var responsePreview string
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for scanner.Scan() {
			data, ok := parseSSEData(scanner.Text())
			if !ok {
				continue
			}
			var chunk caResponseChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			parsed := extractCAChunkFull(chunk)
			if parsed.usage != nil {
				lastUsage = parsed.usage
			}

			// Emit function calls to trigger the tool calling loop
			if len(parsed.toolCalls) > 0 {
				logOAuth.Logf("functionCall: %s", formatToolCallNames(parsed.toolCalls))
				logUsage(lastUsage)
				mmTokens := 0
				if lastUsage != nil {
					mmTokens = lastUsage.TotalTokenCount
				}
				ch <- StreamChunk{ToolCalls: parsed.toolCalls, Done: true, TokensUsed: mmTokens}
				return
			}

			if parsed.text != "" {
				responseLen += len(parsed.text)
				if len(responsePreview) < 80 {
					responsePreview += parsed.text
				}
				ch <- StreamChunk{Content: parsed.text}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err, Done: true}
			return
		}
		logCompletion(lastUsage, responsePreview, responseLen)
		tokens := 0
		if lastUsage != nil {
			tokens = lastUsage.TotalTokenCount
		}
		ch <- StreamChunk{Done: true, TokensUsed: tokens}
	}()

	return ch, nil
}

// parseSSEData extracts the JSON payload from an SSE data line.
// Handles both "data: {...}" and "data:{...}" formats.
func parseSSEData(line string) (string, bool) {
	if strings.HasPrefix(line, "data: ") {
		return strings.TrimPrefix(line, "data: "), true
	}
	if strings.HasPrefix(line, "data:") {
		return strings.TrimPrefix(line, "data:"), true
	}
	return "", false
}

// caChunkResult holds parsed data from a single SSE chunk.
type caChunkResult struct {
	text         string
	tokens       int
	usage        *caUsage // full usage metadata (last chunk only)
	toolCalls    []domain.ToolCall
	finishReason string
}

func extractCAChunkFull(chunk caResponseChunk) caChunkResult {
	result := caChunkResult{}
	if chunk.Response == nil || len(chunk.Response.Candidates) == 0 {
		return result
	}

	candidate := chunk.Response.Candidates[0]
	if candidate.FinishReason != "" {
		result.finishReason = candidate.FinishReason
	}

	var sb strings.Builder
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			result.toolCalls = append(result.toolCalls, domain.ToolCall{
				ID:    part.FunctionCall.Name,
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		}
	}
	result.text = sb.String()

	if chunk.Response.UsageMetadata != nil {
		result.tokens = chunk.Response.UsageMetadata.TotalTokenCount
		result.usage = chunk.Response.UsageMetadata
	}
	return result
}

// logUsage logs token usage summary from the final SSE chunk.
func logUsage(usage *caUsage) {
	if usage == nil {
		return
	}
	logOAuth.Logf("tokens: prompt=%d, response=%d, total=%d",
		usage.PromptTokenCount, usage.CandidatesTokenCount, usage.TotalTokenCount)
}

// logCompletion logs token usage and a preview of the response.
func logCompletion(usage *caUsage, preview string, totalLen int) {
	logUsage(usage)
	// Clean preview: collapse newlines to spaces, truncate
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.ReplaceAll(preview, "\r", "")
	if len(preview) > 80 {
		preview = preview[:80]
	}
	logOAuth.Logf("response: %d chars | %s...", totalLen, strings.TrimSpace(preview))
}

// formatToolCallNames returns a compact string of tool call names.
func formatToolCallNames(calls []domain.ToolCall) string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}
