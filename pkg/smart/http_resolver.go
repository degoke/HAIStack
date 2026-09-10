package smart

import (
	"fmt"
	"net/http"
	"strings"
)

// BearerAuthConfig validates Bearer JWT access tokens and builds AuthBundles for HTTP hosts.
type BearerAuthConfig struct {
	Validator *TokenValidator
	Adapter   *AuthAdapter
	Options   TokenValidateOptions
}

// BearerAuthResult is the authenticated SMART identity for one HTTP request.
type BearerAuthResult struct {
	Bundle AuthBundle
}

// ResolveBearerToken extracts and validates a Bearer token from the Authorization header.
func (c BearerAuthConfig) ResolveBearerToken(r *http.Request) (BearerAuthResult, error) {
	if c.Validator == nil || c.Adapter == nil {
		return BearerAuthResult{}, fmt.Errorf("%w: bearer auth requires Validator and Adapter", ErrInvalidConfig)
	}
	token, err := bearerTokenFromHeader(r.Header.Get("Authorization"))
	if err != nil {
		return BearerAuthResult{}, err
	}
	claims, err := c.Validator.ValidateToken(token, c.Options)
	if err != nil {
		return BearerAuthResult{}, err
	}
	launch := BuildLaunchContext(LaunchContextInput{Claims: &claims, Scopes: claims.Scopes})
	bundle, err := c.Adapter.ToAuthRequests(claims, launch)
	if err != nil {
		return BearerAuthResult{}, err
	}
	return BearerAuthResult{Bundle: bundle}, nil
}

func bearerTokenFromHeader(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("%w: missing Authorization header", ErrUnauthorized)
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("%w: Authorization must use Bearer scheme", ErrUnauthorized)
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", fmt.Errorf("%w: empty bearer token", ErrUnauthorized)
	}
	return token, nil
}
