package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAIStudioEndpoint         = "https://alkalimakersuite-pa.clients6.google.com"
	defaultAIStudioAPIKey           = "AIzaSyDdP816MREB3SkjZO04QXbjsigfcI0GWOs"
	defaultAIStudioOrigin           = "https://aistudio.google.com"
	defaultAIStudioReferer          = "https://aistudio.google.com/"
	defaultAIStudioUserAgent        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	defaultAIStudioAcceptLanguage   = "en-US,en;q=0.9"
	defaultAIStudioBrowserChannel   = "beta"
	defaultAIStudioBrowserCopyright = "Copyright 2026 Google LLC. All Rights reserved."
	defaultAIStudioBrowserYear      = "2026"
	defaultAIStudioGoogExt519733851 = "CAASAUIwATgEQAA="
)

// AIStudioSession stores manually provided AI Studio RPC session headers.
// Values are sensitive and must only be persisted with 0600 permissions.
type AIStudioSession struct {
	Endpoint           string `json:"endpoint"`
	APIKey             string `json:"api_key"`
	XBrowserValidation string `json:"x_browser_validation"`
	XClientData        string `json:"x_client_data"`
	XGoogAuthUser      string `json:"x_goog_authuser"`
	XGoogExt519733851  string `json:"x_goog_ext_519733851_bin,omitempty"`
	RequestState       string `json:"request_state,omitempty"`

	Cookie        string `json:"cookie,omitempty"`
	Authorization string `json:"authorization,omitempty"`

	UserAgent      string `json:"user_agent"`
	AcceptLanguage string `json:"accept_language,omitempty"`
	Origin         string `json:"origin,omitempty"`
	Referer        string `json:"referer,omitempty"`

	XBrowserChannel   string `json:"x_browser_channel,omitempty"`
	XBrowserCopyright string `json:"x_browser_copyright,omitempty"`
	XBrowserYear      string `json:"x_browser_year,omitempty"`
	TimeZone          string `json:"time_zone,omitempty"`
}

// LoadAIStudioSession reads an AI Studio session file. Returns (nil, nil) when
// the file does not exist.
func LoadAIStudioSession(path string) (*AIStudioSession, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read AI Studio session file: %w", err)
	}
	var session AIStudioSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse AI Studio session file: %w", err)
	}
	normalized := normalizeAIStudioSession(&session)
	if err := ValidateAIStudioSession(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// LoadAIStudioRPCProviderFromFile loads a persisted AI Studio session and
// constructs an RPC provider. Returns (nil, nil) when no session exists.
func LoadAIStudioRPCProviderFromFile(path string, model string) (Provider, error) {
	session, err := LoadAIStudioSession(path)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}
	provider, err := NewAIStudioRPCProvider(session, model)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

// SaveAIStudioSession writes an AI Studio session with 0600 permissions.
func SaveAIStudioSession(path string, session *AIStudioSession) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("AI Studio session path is empty")
	}
	normalized := normalizeAIStudioSession(session)
	if err := ValidateAIStudioSession(normalized); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create AI Studio session dir: %w", err)
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal AI Studio session: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write AI Studio session file: %w", err)
	}
	return nil
}

// DeleteAIStudioSession removes the persisted AI Studio session file.
func DeleteAIStudioSession(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete AI Studio session file: %w", err)
	}
	return nil
}

// ValidateAIStudioSession validates required fields for RPC calls.
func ValidateAIStudioSession(session *AIStudioSession) error {
	if session == nil {
		return fmt.Errorf("AI Studio session is nil")
	}
	if strings.TrimSpace(session.Endpoint) == "" {
		return fmt.Errorf("AI Studio session missing endpoint")
	}
	if strings.TrimSpace(session.APIKey) == "" {
		return fmt.Errorf("AI Studio session missing api_key")
	}
	if strings.TrimSpace(session.XBrowserValidation) == "" {
		return fmt.Errorf("AI Studio session missing x_browser_validation")
	}
	if strings.TrimSpace(session.XClientData) == "" {
		return fmt.Errorf("AI Studio session missing x_client_data")
	}
	if strings.TrimSpace(session.XGoogAuthUser) == "" {
		return fmt.Errorf("AI Studio session missing x_goog_authuser")
	}
	if strings.TrimSpace(session.UserAgent) == "" {
		return fmt.Errorf("AI Studio session missing user_agent")
	}
	return nil
}

func normalizeAIStudioSession(session *AIStudioSession) *AIStudioSession {
	if session == nil {
		return nil
	}
	normalized := *session
	normalized.Endpoint = strings.TrimRight(strings.TrimSpace(normalized.Endpoint), "/")
	normalized.APIKey = strings.TrimSpace(normalized.APIKey)
	normalized.XBrowserValidation = strings.TrimSpace(normalized.XBrowserValidation)
	normalized.XClientData = strings.TrimSpace(normalized.XClientData)
	normalized.XGoogAuthUser = strings.TrimSpace(normalized.XGoogAuthUser)
	normalized.XGoogExt519733851 = strings.TrimSpace(normalized.XGoogExt519733851)
	normalized.RequestState = strings.TrimSpace(normalized.RequestState)
	normalized.Cookie = strings.TrimSpace(normalized.Cookie)
	normalized.Authorization = strings.TrimSpace(normalized.Authorization)
	normalized.UserAgent = strings.TrimSpace(normalized.UserAgent)
	normalized.AcceptLanguage = strings.TrimSpace(normalized.AcceptLanguage)
	normalized.Origin = strings.TrimSpace(normalized.Origin)
	normalized.Referer = strings.TrimSpace(normalized.Referer)
	normalized.XBrowserChannel = strings.TrimSpace(normalized.XBrowserChannel)
	normalized.XBrowserCopyright = strings.TrimSpace(normalized.XBrowserCopyright)
	normalized.XBrowserYear = strings.TrimSpace(normalized.XBrowserYear)
	normalized.TimeZone = normalizeAIStudioTimeZone(normalized.TimeZone)

	if normalized.Endpoint == "" {
		normalized.Endpoint = defaultAIStudioEndpoint
	}
	if normalized.APIKey == "" {
		normalized.APIKey = defaultAIStudioAPIKey
	}
	if normalized.XGoogAuthUser == "" {
		normalized.XGoogAuthUser = "0"
	}
	if normalized.XGoogExt519733851 == "" {
		normalized.XGoogExt519733851 = defaultAIStudioGoogExt519733851
	}
	if normalized.UserAgent == "" {
		normalized.UserAgent = defaultAIStudioUserAgent
	}
	if normalized.AcceptLanguage == "" {
		normalized.AcceptLanguage = defaultAIStudioAcceptLanguage
	}
	if normalized.Origin == "" {
		normalized.Origin = defaultAIStudioOrigin
	}
	if normalized.Referer == "" {
		normalized.Referer = defaultAIStudioReferer
	}
	if normalized.XBrowserChannel == "" {
		normalized.XBrowserChannel = defaultAIStudioBrowserChannel
	}
	if normalized.XBrowserCopyright == "" {
		normalized.XBrowserCopyright = defaultAIStudioBrowserCopyright
	}
	if normalized.XBrowserYear == "" {
		normalized.XBrowserYear = defaultAIStudioBrowserYear
	}
	return &normalized
}

func normalizeAIStudioTimeZone(value string) string {
	tz := strings.TrimSpace(value)
	if tz == "" {
		return "UTC"
	}
	if strings.EqualFold(tz, "UTC") {
		return "UTC"
	}
	if strings.HasPrefix(strings.ToLower(tz), "models/") {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}
