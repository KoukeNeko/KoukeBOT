package app

import (
	providerpkg "github.com/KoukeNeko/KoukeBOT/internal/app/provider"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

// Provider facade keeps existing app-level API stable while provider internals
// live in a dedicated package.
type (
	ProviderManager     = providerpkg.ProviderManager
	ProviderPool        = providerpkg.ProviderPool
	PoolStatus          = providerpkg.PoolStatus
	ProviderPoolMetrics = providerpkg.ProviderPoolMetrics
)

var ErrProviderNotConfigured = providerpkg.ErrProviderNotConfigured

func NewProviderManager(initial llm.Provider) *ProviderManager {
	return providerpkg.NewProviderManager(initial)
}

func NewProviderPool() *ProviderPool {
	return providerpkg.NewProviderPool()
}
