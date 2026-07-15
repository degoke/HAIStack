package client

import "context"

// TokenProvider supplies authorization headers for outbound requests.
type TokenProvider interface {
	// AuthorizationHeader returns the full Authorization header value, e.g. "Bearer <token>".
	// Return empty string when no auth is required for the request.
	AuthorizationHeader(ctx context.Context) (string, error)
}

// StaticTokenProvider returns a fixed bearer token.
type StaticTokenProvider struct {
	Token string
}

// AuthorizationHeader implements TokenProvider.
func (p StaticTokenProvider) AuthorizationHeader(_ context.Context) (string, error) {
	if p.Token == "" {
		return "", nil
	}
	return "Bearer " + p.Token, nil
}

// HeaderTokenProvider returns a pre-built Authorization header value.
type HeaderTokenProvider struct {
	Header string
}

// AuthorizationHeader implements TokenProvider.
func (p HeaderTokenProvider) AuthorizationHeader(_ context.Context) (string, error) {
	return p.Header, nil
}
