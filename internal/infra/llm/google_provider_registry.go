package llm

import "strings"

const (
	ProviderGoogleGeminiCLI     = "google-gemini-cli"
	ProviderGoogleAntigravity   = "google-antigravity"
	ProviderGeminiAPIKey        = "gemini-api-key"
	ProviderOpenAIAPIKey        = "openai-api-key"
	ProviderAIStudioRPC         = "aistudio-rpc"
	ProviderAnthropicAPIKey     = "anthropic-api-key"
	ProviderAnthropicSetupToken = "anthropic-setup-token"

	EndpointPolicyPrimaryThenSandbox = "primary_then_sandbox"

	codeAssistPrimaryEndpoint = "https://cloudcode-pa.googleapis.com"
	codeAssistSandboxEndpoint = "https://daily-cloudcode-pa.sandbox.googleapis.com"
)

type GoogleProviderSpec struct {
	ID                 string
	Label              string
	DefaultModel       string
	OAuthScopes        []string
	RedirectURI        string
	EndpointCandidates []string
}

func googleProviderSpec(providerID string) GoogleProviderSpec {
	switch NormalizeGoogleProviderID(providerID) {
	case ProviderAnthropicAPIKey:
		return GoogleProviderSpec{
			ID:           ProviderAnthropicAPIKey,
			Label:        "Anthropic API Key",
			DefaultModel: "claude-sonnet-4-6",
			OAuthScopes:  nil,
			RedirectURI:  "",
			EndpointCandidates: []string{
				defaultAnthropicEndpoint,
			},
		}
	case ProviderAnthropicSetupToken:
		return GoogleProviderSpec{
			ID:           ProviderAnthropicSetupToken,
			Label:        "Anthropic Setup Token",
			DefaultModel: "claude-sonnet-4-6",
			OAuthScopes:  nil,
			RedirectURI:  "",
			EndpointCandidates: []string{
				defaultAnthropicEndpoint,
			},
		}
	case ProviderOpenAIAPIKey:
		return GoogleProviderSpec{
			ID:           ProviderOpenAIAPIKey,
			Label:        "OpenAI API Key",
			DefaultModel: "gpt-5",
			OAuthScopes:  nil,
			RedirectURI:  "",
			EndpointCandidates: []string{
				"https://api.openai.com",
			},
		}
	case ProviderGoogleAntigravity:
		return GoogleProviderSpec{
			ID:           ProviderGoogleAntigravity,
			Label:        "Google Antigravity",
			DefaultModel: "claude-opus-4-6-thinking",
			OAuthScopes: []string{
				"https://www.googleapis.com/auth/cloud-platform",
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
				"https://www.googleapis.com/auth/cclog",
				"https://www.googleapis.com/auth/experimentsandconfigs",
			},
			RedirectURI: "http://localhost:51121/oauth-callback",
			EndpointCandidates: []string{
				codeAssistPrimaryEndpoint,
				codeAssistSandboxEndpoint,
			},
		}
	case ProviderAIStudioRPC:
		return GoogleProviderSpec{
			ID:           ProviderAIStudioRPC,
			Label:        "AI Studio RPC",
			DefaultModel: "gemini-3-pro-preview",
			OAuthScopes:  nil,
			RedirectURI:  "",
			EndpointCandidates: []string{
				defaultAIStudioEndpoint,
			},
		}
	default:
		return GoogleProviderSpec{
			ID:           ProviderGoogleGeminiCLI,
			Label:        "Google Gemini CLI",
			DefaultModel: "gemini-3-pro-preview",
			OAuthScopes: []string{
				"https://www.googleapis.com/auth/cloud-platform",
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
			RedirectURI: "http://localhost:8085/oauth2callback",
			EndpointCandidates: []string{
				codeAssistPrimaryEndpoint,
				codeAssistSandboxEndpoint,
			},
		}
	}
}

func NormalizeGoogleProviderID(providerID string) string {
	normalized := strings.TrimSpace(strings.ToLower(providerID))
	switch normalized {
	case ProviderAnthropicAPIKey:
		return ProviderAnthropicAPIKey
	case ProviderAnthropicSetupToken:
		return ProviderAnthropicSetupToken
	case ProviderOpenAIAPIKey:
		return ProviderOpenAIAPIKey
	case ProviderGoogleAntigravity:
		return ProviderGoogleAntigravity
	case ProviderAIStudioRPC, "aistudio", "ai-studio":
		return ProviderAIStudioRPC
	case ProviderGeminiAPIKey:
		return ProviderGeminiAPIKey
	case ProviderGoogleGeminiCLI, "gemini-cli", "oauth", "cli", "":
		return ProviderGoogleGeminiCLI
	default:
		return normalized
	}
}

func NormalizeGoogleModelAlias(model string) string {
	// Legacy/global alias normalizer kept for backward compatibility.
	// New code should prefer NormalizeModelAliasForProvider.
	normalized := NormalizeModelID(model)
	switch normalized {
	case "gemini-3-pro":
		return "gemini-3-pro-preview"
	case "gemini-3-flash":
		return "gemini-3-flash-preview"
	case "claude-opus-4-6":
		return "claude-opus-4-6-thinking"
	case "claude-sonnet-4":
		return "claude-sonnet-4-thinking"
	default:
		return normalized
	}
}

func NormalizeModelAliasForProvider(providerID, model string) string {
	pid := NormalizeGoogleProviderID(providerID)
	normalized := NormalizeModelID(model)
	switch normalized {
	case "gemini-3-pro":
		normalized = "gemini-3-pro-preview"
	case "gemini-3-flash":
		normalized = "gemini-3-flash-preview"
	}

	switch pid {
	case ProviderGoogleAntigravity, "":
		// Keep OpenClaw-compatible aliases for Google Antigravity only.
		switch normalized {
		case "claude-opus-4-6":
			return "claude-opus-4-6-thinking"
		case "claude-sonnet-4":
			return "claude-sonnet-4-thinking"
		}
	}
	return normalized
}

func BuildProviderModelID(providerID, model string) string {
	pid := NormalizeGoogleProviderID(providerID)
	mid := NormalizeModelAliasForProvider(pid, model)
	if pid == "" {
		pid = ProviderGoogleGeminiCLI
	}
	if mid == "" {
		mid = googleProviderSpec(pid).DefaultModel
	}
	return pid + "/" + mid
}

func SplitProviderModelID(modelID string) (providerID, model string) {
	normalized := strings.TrimSpace(modelID)
	if normalized == "" {
		return "", ""
	}
	if idx := strings.Index(normalized, "/"); idx > 0 {
		pid := NormalizeGoogleProviderID(normalized[:idx])
		mid := NormalizeModelAliasForProvider(pid, normalized[idx+1:])
		return pid, mid
	}
	return "", NormalizeGoogleModelAlias(normalized)
}

func ResolveProviderAndModel(providerID, modelID, legacyModel string) (string, string, string) {
	pid := NormalizeGoogleProviderID(providerID)
	if pid == "" {
		pid = ProviderGoogleGeminiCLI
	}
	if pFromModel, mFromModel := SplitProviderModelID(modelID); mFromModel != "" {
		if pFromModel != "" {
			pid = pFromModel
		}
		mid := NormalizeModelAliasForProvider(pid, mFromModel)
		return pid, mid, BuildProviderModelID(pid, mid)
	}
	if legacy := NormalizeModelAliasForProvider(pid, legacyModel); legacy != "" {
		return pid, legacy, BuildProviderModelID(pid, legacy)
	}
	def := googleProviderSpec(pid).DefaultModel
	return pid, def, BuildProviderModelID(pid, def)
}

func DefaultProviderModelID(providerID string) string {
	pid := NormalizeGoogleProviderID(providerID)
	if pid == "" {
		pid = ProviderGoogleGeminiCLI
	}
	return BuildProviderModelID(pid, googleProviderSpec(pid).DefaultModel)
}

func ResolveEndpointPolicy(policy string) string {
	switch strings.TrimSpace(strings.ToLower(policy)) {
	case EndpointPolicyPrimaryThenSandbox:
		return EndpointPolicyPrimaryThenSandbox
	default:
		return EndpointPolicyPrimaryThenSandbox
	}
}
