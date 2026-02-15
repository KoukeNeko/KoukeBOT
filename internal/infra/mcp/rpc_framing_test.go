package mcp

import "testing"

func TestParseSSEEvents_MultiEventAndMultilineData(t *testing.T) {
	body := []byte("event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n" +
		"event: notice\n" +
		"data: line-1\n" +
		"data: line-2\n\n")

	events := parseSSEEvents(body)
	if len(events) != 2 {
		t.Fatalf("events length = %d, want 2", len(events))
	}
	if string(events[0].Data) != `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` {
		t.Fatalf("unexpected first data: %s", string(events[0].Data))
	}
	if string(events[1].Data) != "line-1\nline-2" {
		t.Fatalf("unexpected second data: %q", string(events[1].Data))
	}
}

func TestSelectSSEDataPayloadForRequest_MatchByID(t *testing.T) {
	body := []byte("event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{\"skip\":true}}\n\n" +
		"event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"list-fonts\"}]}}\n\n")

	payload, err := selectSSEDataPayloadForRequest(body, int64(2))
	if err != nil {
		t.Fatalf("selectSSEDataPayloadForRequest error: %v", err)
	}
	want := `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"list-fonts"}]}}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", string(payload), want)
	}
}

func TestSelectSSEDataPayloadForRequest_NoMatch(t *testing.T) {
	body := []byte("event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")

	if _, err := selectSSEDataPayloadForRequest(body, int64(7)); err == nil {
		t.Fatalf("expected error for unmatched request id")
	}
}
