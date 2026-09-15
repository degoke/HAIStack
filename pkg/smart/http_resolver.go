package smart

import (
	"context"
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

// BackendAssertionAuthConfig validates backend-service client assertions for HTTP hosts.
type BackendAssertionAuthConfig struct {
	Backend *BackendServiceAuth
	Adapter *AuthAdapter
	Options TokenValidateOptions
}

// BearerAuthResult is the authenticated SMART identity for one HTTP request.
type BearerAuthResult struct {
	Bundle AuthBundle
}

type bearerAuthCache struct {
	resolved bool
	result   BearerAuthResult
	err      error
}

type bearerAuthCacheKey struct{}

// ContextWithBearerAuthCache attaches a per-request bearer auth cache to ctx.
func ContextWithBearerAuthCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, bearerAuthCacheKey{}, &bearerAuthCache{})
}

// ResolveBearerToken extracts and validates a Bearer token from the Authorization header.
func (c BearerAuthConfig) ResolveBearerToken(r *http.Request) (BearerAuthResult, error) {
	return c.ResolveBearerTokenCached(r.Context(), r)
}

// ResolveBearerTokenCached validates the Bearer token at most once per request context.
func (c BearerAuthConfig) ResolveBearerTokenCached(ctx context.Context, r *http.Request) (BearerAuthResult, error) {
	if cache, ok := ctx.Value(bearerAuthCacheKey{}).(*bearerAuthCache); ok && cache.resolved {
		return cache.result, cache.err
	}
	result, err := c.resolveBearerToken(r)
	if cache, ok := ctx.Value(bearerAuthCacheKey{}).(*bearerAuthCache); ok {
		cache.result = result
		cache.err = err
		cache.resolved = true
	}
	return result, err
}

func (c BearerAuthConfig) resolveBearerToken(r *http.Request) (BearerAuthResult, error) {
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

// ResolveBackendAssertionCached validates a backend assertion at most once per request context.
func (c BackendAssertionAuthConfig) ResolveBackendAssertionCached(ctx context.Context, r *http.Request) (BearerAuthResult, error) {
	if cache, ok := ctx.Value(bearerAuthCacheKey{}).(*bearerAuthCache); ok && cache.resolved {
		return cache.result, cache.err
	}
	result, err := c.resolveBackendAssertion(r)
	if cache, ok := ctx.Value(bearerAuthCacheKey{}).(*bearerAuthCache); ok {
		cache.result = result
		cache.err = err
		cache.resolved = true
	}
	return result, err
}

func (c BackendAssertionAuthConfig) resolveBackendAssertion(r *http.Request) (BearerAuthResult, error) {
	if c.Backend == nil || c.Adapter == nil {
		return BearerAuthResult{}, fmt.Errorf("%w: backend assertion auth requires Backend and Adapter", ErrInvalidConfig)
	}
	token, err := bearerTokenFromHeader(r.Header.Get("Authorization"))
	if err != nil {
		return BearerAuthResult{}, err
	}
	claims, client, err := c.Backend.ValidateBackendAssertion(token, c.Options)
	if err != nil {
		return BearerAuthResult{}, err
	}
	bundle, err := c.Adapter.FromBackendService(claims, client, LaunchContext{})
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
