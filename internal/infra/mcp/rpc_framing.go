package mcp

import (
	"bufio"
	"bytes"
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

func parseSSEDataPayload(body []byte) []byte {
	lines := bytes.Split(body, []byte("\n"))
	var chunks [][]byte
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			chunk := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(chunk) == 0 || bytes.Equal(chunk, []byte("[DONE]")) {
				continue
			}
			chunks = append(chunks, chunk)
		}
	}
	if len(chunks) == 0 {
		return nil
	}
	return chunks[len(chunks)-1]
}
