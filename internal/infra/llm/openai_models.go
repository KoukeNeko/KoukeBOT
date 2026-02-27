package llm

// ListOpenAIModels returns a curated OpenAI catalog for API-key mode.
func ListOpenAIModels() []ModelEntry {
	return normalizeModelEntries([]ModelEntry{
		{
			ID:                 BuildProviderModelID(ProviderOpenAIAPIKey, "gpt-5"),
			DisplayName:        "OpenAI / GPT-5",
			ProviderID:         ProviderOpenAIAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderOpenAIAPIKey, "gpt-5-mini"),
			DisplayName:        "OpenAI / GPT-5 Mini",
			ProviderID:         ProviderOpenAIAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
		{
			ID:                 BuildProviderModelID(ProviderOpenAIAPIKey, "gpt-5-nano"),
			DisplayName:        "OpenAI / GPT-5 Nano",
			ProviderID:         ProviderOpenAIAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          false,
		},
		{
			ID:                 BuildProviderModelID(ProviderOpenAIAPIKey, "gpt-4.1"),
			DisplayName:        "OpenAI / GPT-4.1",
			ProviderID:         ProviderOpenAIAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          false,
		},
		{
			ID:                 BuildProviderModelID(ProviderOpenAIAPIKey, "o4-mini"),
			DisplayName:        "OpenAI / o4-mini",
			ProviderID:         ProviderOpenAIAPIKey,
			SupportsText:       true,
			SupportsMultimodal: true,
			SupportsTools:      true,
			Reasoning:          true,
		},
	})
}
