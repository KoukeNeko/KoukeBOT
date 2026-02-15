package llm

import "context"

const (
	RequestSourceDiscord = "discord"
	RequestSourceWeb     = "web"
	RequestSourceAPI     = "api"
)

// RequestContext carries per-request metadata needed by runtime-bound tools.
type RequestContext struct {
	Source    string
	GuildID   string
	ChannelID string
	UserID    string
	PersonaID string
}

type requestContextKey struct{}

// WithRequestContext attaches request metadata to context.
func WithRequestContext(ctx context.Context, req RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey{}, req)
}

// GetRequestContext retrieves request metadata from context.
func GetRequestContext(ctx context.Context) (RequestContext, bool) {
	if ctx == nil {
		return RequestContext{}, false
	}
	v := ctx.Value(requestContextKey{})
	req, ok := v.(RequestContext)
	return req, ok
}
