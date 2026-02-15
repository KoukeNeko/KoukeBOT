package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
	interfacecommon "github.com/KoukeNeko/KoukeBOT/internal/interface/shared"
)

// --- Chat endpoint ---

type ChatRequest struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ChannelID      string `json:"channel_id"`
	PersonaID      string `json:"persona_id"`
	Message        string `json:"message"`
}

type ChatResponseBody struct {
	Response string `json:"response"`
}

func (h *Handlers) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := llm.WithRequestContext(r.Context(), llm.RequestContext{Source: llm.RequestSourceAPI})

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	conversationID, channelID, personaID, err := h.resolveConversationRouting(ctx, req.ConversationID, req.ChannelID, req.PersonaID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	logAPI.Logf("chat: channel=%s persona=%s conversation=%s msg=%s",
		channelID, personaID, conversationID, truncate(req.Message, 50))

	var response string
	if conversationID != "" {
		response, err = h.chatService.HandleMessageByConversationID(ctx, conversationID, req.Message)
	} else {
		response, err = h.chatService.HandleMessage(ctx, channelID, personaID, req.Message)
	}
	if err != nil {
		logAPI.Errorf("chat: %v", err)
		// Distinguish between configuration errors and actual server errors
		if strings.Contains(err.Error(), "LLM provider not configured") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	logAPI.Logf("chat: response length=%d chars", len(response))
	writeJSON(w, http.StatusOK, ChatResponseBody{Response: response})
}

// ChatStream handles Server-Sent Events streaming for real-time chat responses.
// GET /api/chat/stream?persona_id=xxx&message=xxx&channel_id=web-test&conversation_id=... (text only)
// POST /api/chat/stream with multipart/form-data (text + images + optional conversation_id)
func (h *Handlers) ChatStream(w http.ResponseWriter, r *http.Request) {
	ctx := llm.WithRequestContext(r.Context(), llm.RequestContext{Source: llm.RequestSourceAPI})

	var personaID, message, channelID, conversationID string
	var images []domain.ImageInput

	// Parse request based on method
	if r.Method == http.MethodGet {
		// GET: Query params (text only)
		personaID = r.URL.Query().Get("persona_id")
		message = r.URL.Query().Get("message")
		channelID = r.URL.Query().Get("channel_id")
		conversationID = r.URL.Query().Get("conversation_id")
	} else if r.Method == http.MethodPost {
		// POST: Multipart form data (text + images)
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB max
			writeError(w, http.StatusBadRequest, "parse multipart form: "+err.Error())
			return
		}

		personaID = r.FormValue("persona_id")
		message = r.FormValue("message")
		channelID = r.FormValue("channel_id")
		conversationID = r.FormValue("conversation_id")

		// Parse images
		if files := r.MultipartForm.File["images"]; len(files) > 0 {
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					writeError(w, http.StatusBadRequest, "open image file: "+err.Error())
					return
				}
				defer file.Close()

				// Read file content
				data := make([]byte, fileHeader.Size)
				if _, err := file.Read(data); err != nil {
					writeError(w, http.StatusBadRequest, "read image file: "+err.Error())
					return
				}

				// Validate MIME type
				mimeType := fileHeader.Header.Get("Content-Type")
				if !isValidImageMIME(mimeType) {
					writeError(w, http.StatusBadRequest, "invalid image type: "+mimeType)
					return
				}

				images = append(images, domain.ImageInput{
					Data:     data,
					MimeType: mimeType,
					Filename: fileHeader.Filename,
				})
			}
		}
	} else {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if message == "" {
		writeError(w, http.StatusBadRequest, "missing message")
		return
	}

	conversationID, channelID, personaID, err := h.resolveConversationRouting(ctx, conversationID, channelID, personaID)
	if err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	logAPI.Logf("chat-stream: channel=%s persona=%s conversation=%s msg=%s images=%d",
		channelID, personaID, conversationID, truncate(message, 50), len(images))

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Start streaming (with or without images)
	var streamCh <-chan llm.StreamChunk
	if len(images) > 0 {
		if conversationID != "" {
			streamCh, err = h.chatService.HandleMessageStreamWithImagesByConversationID(ctx, conversationID, message, images)
		} else {
			streamCh, err = h.chatService.HandleMessageStreamWithImages(ctx, channelID, personaID, message, images)
		}
	} else {
		if conversationID != "" {
			streamCh, err = h.chatService.HandleMessageStreamByConversationID(ctx, conversationID, message)
		} else {
			streamCh, err = h.chatService.HandleMessageStream(ctx, channelID, personaID, message)
		}
	}

	if err != nil {
		logAPI.Errorf("chat-stream: init error: %v", err)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Forward chunks as SSE events
	totalChunks := 0
	for chunk := range streamCh {
		if chunk.Error != nil {
			logAPI.Errorf("chat-stream: %v", chunk.Error)
			// Format user-friendly error message
			userFriendlyMsg := formatUserFriendlyError(chunk.Error)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", userFriendlyMsg)
			flusher.Flush()
			return
		}

		// Handle tool status events
		if chunk.ToolStatus != "" {
			toolData := map[string]interface{}{
				"status": chunk.ToolStatus,
				"name":   chunk.ToolName,
			}
			if chunk.ToolInput != nil {
				toolData["input"] = chunk.ToolInput
			}
			if chunk.ToolError != "" {
				toolData["error"] = chunk.ToolError
			}
			toolJSON, _ := json.Marshal(toolData)
			fmt.Fprintf(w, "event: tool\ndata: %s\n\n", string(toolJSON))
			flusher.Flush()
			continue
		}

		// Regular content chunk
		if chunk.Content != "" {
			// Escape newlines for SSE format
			content := strings.ReplaceAll(chunk.Content, "\n", "\\n")
			fmt.Fprintf(w, "data: %s\n\n", content)
			flusher.Flush()
			totalChunks++
		}
	}

	// Send done event
	logAPI.Logf("chat-stream: streamed %d chunks", totalChunks)
	fmt.Fprintf(w, "event: done\ndata: \n\n")
	flusher.Flush()
}

func (h *Handlers) resolveConversationRouting(ctx context.Context, conversationID, channelID, personaID string) (resolvedConversationID, resolvedChannelID, resolvedPersonaID string, err error) {
	if strings.TrimSpace(conversationID) == "" {
		if channelID == "" {
			channelID = "web-test"
		}
		if personaID == "" {
			return "", "", "", fmt.Errorf("missing persona_id or conversation_id")
		}
		return "", channelID, personaID, nil
	}

	conv, err := h.convStore.GetByID(ctx, conversationID)
	if err != nil {
		return "", "", "", fmt.Errorf("get conversation %s: %w", conversationID, err)
	}
	if conv == nil {
		return "", "", "", fmt.Errorf("%w: %s", store.ErrConversationNotFound, conversationID)
	}
	if personaID != "" && personaID != conv.PersonaID {
		return "", "", "", fmt.Errorf("conversation_id/persona_id mismatch: %s != %s", conv.PersonaID, personaID)
	}
	if channelID != "" && channelID != conv.ChannelID {
		return "", "", "", fmt.Errorf("conversation_id/channel_id mismatch: %s != %s", conv.ChannelID, channelID)
	}
	return conv.ID, conv.ChannelID, conv.PersonaID, nil
}

// formatUserFriendlyError converts technical errors to user-friendly messages.
func formatUserFriendlyError(err error) string {
	parsed := interfacecommon.ParseLLMError(err)
	switch parsed.Kind {
	case interfacecommon.ErrorKindQuota:
		if parsed.ResetAfter != "" {
			return fmt.Sprintf("⏳ API 配額已用完，請等待 %s 後重試\n\n💡 提示：可以切換到 API Key 模式或等待配額重置", parsed.ResetAfter)
		}
		return "⏳ API 配額已用完，請稍後再試\n\n💡 提示：請至 Settings 切換到 API Key 模式，或等待配額重置"
	case interfacecommon.ErrorKindAuth:
		return "🔐 認證失敗\n\n💡 提示：請至 Settings 重新設定 API Key 或 OAuth"
	case interfacecommon.ErrorKindNetwork:
		return "🌐 網路連線失敗\n\n💡 提示：請檢查網路連線或稍後再試"
	case interfacecommon.ErrorKindRateLimit:
		return "⚠️ 請求過於頻繁\n\n💡 提示：請稍等片刻後再試"
	}

	// Generic error fallback
	return fmt.Sprintf("❌ 發生錯誤：%s\n\n💡 提示：請查看後端 log 或至 Settings 檢查設定", parsed.RawMessage)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isValidImageMIME checks if the MIME type is a supported image format.
func isValidImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
