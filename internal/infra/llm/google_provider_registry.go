package llm

import "strings"

const (
	ProviderGoogleGeminiCLI   = "google-gemini-cli"
	ProviderGoogleAntigravity = "google-antigravity"
	ProviderGeminiAPIKey      = "gemini-api-key"

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
	case ProviderGoogleAntigravity:
		return ProviderGoogleAntigravity
	case ProviderGeminiAPIKey:
		return ProviderGeminiAPIKey
	case ProviderGoogleGeminiCLI, "gemini-cli", "oauth", "cli", "":
		return ProviderGoogleGeminiCLI
	default:
		return normalized
	}
}

func NormalizeGoogleModelAlias(model string) string {
	normalized := NormalizeModelID(model)
	switch normalized {
	case "gemini-3-pro":
		return "gemini-3-pro-preview"
	case "gemini-3-flash":
		return "gemini-3-flash-preview"
	default:
		return normalized
	}
}

func BuildProviderModelID(providerID, model string) string {
	pid := NormalizeGoogleProviderID(providerID)
	mid := NormalizeGoogleModelAlias(model)
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
		mid := NormalizeGoogleModelAlias(normalized[idx+1:])
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
		mid := NormalizeGoogleModelAlias(mFromModel)
		return pid, mid, BuildProviderModelID(pid, mid)
	}
	if legacy := NormalizeGoogleModelAlias(legacyModel); legacy != "" {
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
