package llm

import (
	"regexp"
	"sync"
	"time"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
)

var logFlow = logger.New("oauth", logger.Yellow)

const (
	oauthAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	oauthTokenURL    = "https://oauth2.googleapis.com/token"
	oauthUserInfoURL = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	codeAssistURL    = "https://cloudcode-pa.googleapis.com"

	tokenExpiryMargin = 5 * time.Minute
	pollMaxAttempts   = 24
	pollInterval      = 5 * time.Second
)

var oauthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

var (
	clientIDRegex     = regexp.MustCompile(`(\d+-[a-z0-9]+\.apps\.googleusercontent\.com)`)
	clientSecretRegex = regexp.MustCompile(`(GOCSPX-[A-Za-z0-9_-]+)`)
)

// OAuthTokenData is persisted to data/gemini_oauth.json.
type OAuthTokenData struct {
	ClientID     string           `json:"client_id"`
	ClientSecret string           `json:"client_secret"`
	AccessToken  string           `json:"access_token"`
	RefreshToken string           `json:"refresh_token"`
	TokenType    string           `json:"token_type"`
	Expiry       time.Time        `json:"expiry"`
	ProjectID    string           `json:"project_id"`
	Email        string           `json:"email,omitempty"`
	ProviderID   string           `json:"provider_id,omitempty"`
	TierID       string           `json:"tier_id,omitempty"`
	EndpointUsed string           `json:"endpoint_used,omitempty"`
	UsageStats   *OAuthUsageStats `json:"usage_stats,omitempty"`
}

// OAuthUsageBucket stores aggregated account usage for one hour.
type OAuthUsageBucket struct {
	HourStart time.Time `json:"hour_start"`
	Attempts  int       `json:"attempts"`
	Successes int       `json:"successes"`
	Failures  int       `json:"failures"`
}

// OAuthUsageStats stores persisted usage counters for one OAuth account.
type OAuthUsageStats struct {
	AttemptsTotal  int                `json:"attempts_total"`
	SuccessesTotal int                `json:"successes_total"`
	FailuresTotal  int                `json:"failures_total"`
	HourlyBuckets  []OAuthUsageBucket `json:"hourly_buckets,omitempty"`
	LastAttemptAt  time.Time          `json:"last_attempt_at,omitempty"`
}

type oauthFlowState struct {
	FlowID       string
	State        string
	CodeVerifier string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Mode         string
	ProviderID   string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// OAuthStartInfo contains metadata for an OAuth flow creation response.
type OAuthStartInfo struct {
	FlowID      string    `json:"flow_id"`
	AuthURL     string    `json:"auth_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Mode        string    `json:"mode"`
	RedirectURL string    `json:"redirect_url,omitempty"`
	ProviderID  string    `json:"provider_id,omitempty"`
}

// GeminiOAuthFlow manages the entire OAuth lifecycle.
type GeminiOAuthFlow struct {
	mu          sync.Mutex
	tokenPath   string
	poolPath    string
	redirectURL string
	flowTTL     time.Duration
	flows       map[string]*oauthFlowState
}

// NewGeminiOAuthFlow creates a new flow manager.
func NewGeminiOAuthFlow(tokenPath, poolPath, redirectURL string) *GeminiOAuthFlow {
	return &GeminiOAuthFlow{
		tokenPath:   tokenPath,
		poolPath:    poolPath,
		redirectURL: redirectURL,
		flowTTL:     10 * time.Minute,
		flows:       make(map[string]*oauthFlowState),
	}
}

// PoolPath returns the path to the OAuth pool file.
func (f *GeminiOAuthFlow) PoolPath() string {
	return f.poolPath
}
