package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func writeFramedJSON(w io.Writer, payload []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFramedJSON(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "Content-Length:"))
			if value == trimmed {
				value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(trimmed), "content-length:"))
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid content-length %q: %w", value, err)
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type sseEvent struct {
	Event string
	Data  []byte
}

func parseSSEEvents(body []byte) []sseEvent {
	if len(body) == 0 {
		return nil
	}

	var events []sseEvent
	var currentType string
	var currentData bytes.Buffer

	flush := func() {
		if currentData.Len() == 0 {
			currentType = ""
			return
		}
		payload := bytes.TrimSpace(currentData.Bytes())
		if len(payload) == 0 {
			currentType = ""
			currentData.Reset()
			return
		}
		event := sseEvent{
			Event: strings.TrimSpace(currentType),
			Data:  append([]byte(nil), payload...),
		}
		events = append(events, event)
		currentType = ""
		currentData.Reset()
	}

	lines := bytes.Split(body, []byte("\n"))
	for _, raw := range lines {
		line := bytes.TrimRight(raw, "\r")
		if len(line) == 0 {
			flush()
			continue
		}
		if bytes.HasPrefix(line, []byte(":")) {
			continue
		}
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			currentType = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
		case bytes.HasPrefix(line, []byte("data:")):
			chunk := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if currentData.Len() > 0 {
				currentData.WriteByte('\n')
			}
			currentData.Write(chunk)
		}
	}
	flush()

	return events
}

func selectSSEDataPayloadForRequest(body []byte, requestID interface{}) ([]byte, error) {
	events := parseSSEEvents(body)
	if len(events) == 0 {
		return nil, fmt.Errorf("empty SSE payload")
	}

	var lastData []byte
	for _, event := range events {
		data := bytes.TrimSpace(event.Data)
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		lastData = data
		if requestID == nil {
			continue
		}

		var response rpcResponse
		if err := json.Unmarshal(data, &response); err != nil {
			continue
		}
		if fmt.Sprint(response.ID) != fmt.Sprint(requestID) {
			continue
		}
		return data, nil
	}

	if requestID == nil && len(lastData) > 0 {
		return lastData, nil
	}
	if len(lastData) == 0 {
		return nil, fmt.Errorf("no data events in SSE payload")
	}
	return nil, fmt.Errorf("no matching RPC response for id %v", requestID)
}
