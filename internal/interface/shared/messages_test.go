package shared

import (
	"errors"
	"testing"
)

func TestParseLLMErrorClassifiesContextDeadlineExceededAsNetwork(t *testing.T) {
	parsed := ParseLLMError(errors.New("llm multimodal stream: context deadline exceeded"))
	if parsed.Kind != ErrorKindNetwork {
		t.Fatalf("kind = %v, want %v", parsed.Kind, ErrorKindNetwork)
	}
}

func TestParseLLMErrorClassifiesContextCanceledAsNetwork(t *testing.T) {
	parsed := ParseLLMError(errors.New("request aborted: context canceled"))
	if parsed.Kind != ErrorKindNetwork {
		t.Fatalf("kind = %v, want %v", parsed.Kind, ErrorKindNetwork)
	}
}

func TestParseLLMErrorCaseInsensitiveNetworkMatching(t *testing.T) {
	parsed := ParseLLMError(errors.New("TIMEOUT while reading NETWORK response"))
	if parsed.Kind != ErrorKindNetwork {
		t.Fatalf("kind = %v, want %v", parsed.Kind, ErrorKindNetwork)
	}
}

func TestParseLLMErrorClassifiesQueueTimeout(t *testing.T) {
	parsed := ParseLLMError(errors.New("all OAuth account(s) exhausted (queue retry failed: request timed out waiting in queue)"))
	if parsed.Kind != ErrorKindQueueTimeout {
		t.Fatalf("kind = %v, want %v", parsed.Kind, ErrorKindQueueTimeout)
	}
}
