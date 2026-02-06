package app

import (
	"context"
	"errors"
	"sync"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// ErrProviderNotConfigured is returned when no LLM provider has been set.
var ErrProviderNotConfigured = errors.New("LLM provider not configured — set your API key in Settings")

// ProviderManager wraps an llm.Provider with thread-safe hot-swap support.
// It implements llm.Provider itself so callers (ChatService) are unaware of the swap mechanism.
type ProviderManager struct {
	mu       sync.RWMutex
	provider llm.Provider
}

func NewProviderManager(initial llm.Provider) *ProviderManager {
	return &ProviderManager{provider: initial}
}

func (pm *ProviderManager) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	pm.mu.RLock()
	p := pm.provider
	pm.mu.RUnlock()

	if p == nil {
		return nil, ErrProviderNotConfigured
	}
	return p.Chat(ctx, req)
}

func (pm *ProviderManager) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	pm.mu.RLock()
	p := pm.provider
	pm.mu.RUnlock()

	if p == nil {
		return nil, ErrProviderNotConfigured
	}
	return p.StreamChat(ctx, req)
}

func (pm *ProviderManager) ModelInfo() llm.ModelCapabilities {
	pm.mu.RLock()
	p := pm.provider
	pm.mu.RUnlock()

	if p == nil {
		return llm.ModelCapabilities{ProviderName: "not configured"}
	}
	return p.ModelInfo()
}

// Update replaces the active provider. Pass nil to clear.
func (pm *ProviderManager) Update(p llm.Provider) {
	pm.mu.Lock()
	pm.provider = p
	pm.mu.Unlock()
}

// IsConfigured returns true if a provider has been set.
func (pm *ProviderManager) IsConfigured() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.provider != nil
}
