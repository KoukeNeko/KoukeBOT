package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

func TestCreateConversationCreatesNewSessionAndActivatesLatest(t *testing.T) {
	convStore := store.NewMemoryStore()
	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/conversations", bytes.NewBufferString(`{"persona_id":"persona-a"}`))
		rec := httptest.NewRecorder()
		h.CreateConversation(rec, req)
		return rec
	}

	rec1 := makeReq()
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d", rec1.Code, http.StatusCreated)
	}
	var conv1 domain.Conversation
	if err := json.Unmarshal(rec1.Body.Bytes(), &conv1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if conv1.ID == "" {
		t.Fatal("expected non-empty conversation id")
	}
	if conv1.ChannelID != "web-test" {
		t.Fatalf("first channel_id = %q, want web-test", conv1.ChannelID)
	}

	rec2 := makeReq()
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second create status = %d, want %d", rec2.Code, http.StatusCreated)
	}
	var conv2 domain.Conversation
	if err := json.Unmarshal(rec2.Body.Bytes(), &conv2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if conv2.ID == "" {
		t.Fatal("expected non-empty conversation id")
	}
	if conv1.ID == conv2.ID {
		t.Fatalf("expected different conversation ids, both=%s", conv1.ID)
	}

	activeID, err := convStore.GetActiveID(context.Background(), "web-test", "persona-a")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != conv2.ID {
		t.Fatalf("active id = %s, want latest %s", activeID, conv2.ID)
	}
}

func TestCreateConversationMissingPersonaIDReturnsBadRequest(t *testing.T) {
	convStore := store.NewMemoryStore()
	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}

	req := httptest.NewRequest("POST", "/api/conversations", bytes.NewBufferString(`{"channel_id":"web-test"}`))
	rec := httptest.NewRecorder()
	h.CreateConversation(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListConversationsIncludesResetConversation(t *testing.T) {
	convStore := store.NewMemoryStore()
	now := time.Date(2026, time.February, 14, 23, 30, 0, 0, time.UTC)
	cutoff := now.Add(-5 * time.Minute)

	if err := convStore.Save(context.Background(), &domain.Conversation{
		ID:              "ch-reset:persona-a",
		ChannelID:       "ch-reset",
		PersonaID:       "persona-a",
		Messages:        []domain.Message{},
		HistoryCutoffAt: &cutoff,
		CreatedAt:       now.Add(-time.Hour),
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("save reset conversation: %v", err)
	}

	if err := convStore.Save(context.Background(), &domain.Conversation{
		ID:        "ch-empty:persona-b",
		ChannelID: "ch-empty",
		PersonaID: "persona-b",
		Messages:  []domain.Message{},
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save empty conversation: %v", err)
	}

	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}

	req := httptest.NewRequest("GET", "/api/conversations", nil)
	rec := httptest.NewRecorder()
	h.ListConversations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got []ConversationSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	foundReset := false
	foundEmpty := false
	for _, item := range got {
		if item.ChannelID == "ch-reset" && item.PersonaID == "persona-a" {
			foundReset = true
			if item.MessageCount != 0 {
				t.Fatalf("reset conversation message_count = %d, want 0", item.MessageCount)
			}
			if item.LastMessage != "[對話已重置]" {
				t.Fatalf("reset conversation last_message = %q, want reset marker", item.LastMessage)
			}
			if !item.IsActive {
				t.Fatalf("expected reset conversation to be active")
			}
		}
		if item.ChannelID == "ch-empty" && item.PersonaID == "persona-b" {
			foundEmpty = true
		}
	}

	if !foundReset {
		t.Fatalf("expected reset conversation to be listed")
	}
	if foundEmpty {
		t.Fatalf("expected plain empty conversation to be hidden")
	}
}

func TestResolveConversationRoutingRejectsPersonaMismatch(t *testing.T) {
	convStore := store.NewMemoryStore()
	conv, err := convStore.CreateSession(context.Background(), "ch-1", "persona-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}
	_, _, _, err = h.resolveConversationRouting(context.Background(), conv.ID, "ch-1", "persona-b")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "conversation_id/persona_id mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteConversationByIDActiveCreatesReplacementSession(t *testing.T) {
	convStore := store.NewMemoryStore()
	conv, err := convStore.CreateSession(context.Background(), "ch-1", "persona-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	conv.Messages = append(conv.Messages, domain.Message{
		Role:      domain.RoleUser,
		Content:   "hello",
		Timestamp: time.Now().UTC(),
	})
	if err := convStore.Save(context.Background(), conv); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	h := &Handlers{
		convStore: convStore,
		ctx:       context.Background(),
	}
	req := httptest.NewRequest("DELETE", "/api/conversations/by-id/"+conv.ID, nil)
	req.SetPathValue("conversationID", conv.ID)
	rec := httptest.NewRecorder()
	h.DeleteConversationByID(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	newID, _ := body["new_conversation_id"].(string)
	if newID == "" {
		t.Fatalf("expected replacement new_conversation_id, body=%v", body)
	}
	if newID == conv.ID {
		t.Fatalf("expected new session id different from deleted id")
	}

	activeID, err := convStore.GetActiveID(context.Background(), "ch-1", "persona-a")
	if err != nil {
		t.Fatalf("GetActiveID failed: %v", err)
	}
	if activeID != newID {
		t.Fatalf("active id = %s, want %s", activeID, newID)
	}
}
