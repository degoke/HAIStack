package oauth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/degoke/haistack/pkg/auth"
	hahttp "github.com/degoke/haistack/pkg/http"
	"github.com/degoke/haistack/pkg/smart"
)

// MultiTenantBearerAuth validates Bearer tokens issued by the base OAuth server or
// any registered tenant issuer.
type MultiTenantBearerAuth struct {
	Base    *Server
	Tenants *MultiTenantServer
	Adapter *smart.AuthAdapter
}

// BearerConfigForRequest selects the bearer validation config for the request token issuer.
func (m MultiTenantBearerAuth) BearerConfigForRequest(r *http.Request) (smart.BearerAuthConfig, error) {
	if m.Base == nil || m.Adapter == nil {
		return smart.BearerAuthConfig{}, fmt.Errorf("%w: multi-tenant bearer auth is not configured", ErrInvalidConfig)
	}
	token, err := bearerTokenFromRequest(r)
	if err != nil {
		return smart.BearerAuthConfig{}, err
	}
	unverified, err := smart.ParseTokenUnverified(token)
	if err != nil {
		return smart.BearerAuthConfig{}, err
	}
	if m.Tenants != nil {
		tenantCfg, err := m.Tenants.LookupByIssuer(unverified.Issuer)
		if err == nil {
			srv, err := m.Tenants.ServerForTenant(tenantCfg.TenantID)
			if err != nil {
				return smart.BearerAuthConfig{}, err
			}
			return srv.BearerAuthConfig(m.Adapter), nil
		}
	}
	if unverified.Issuer == m.Base.Issuer() {
		return m.Base.BearerAuthConfig(m.Adapter), nil
	}
	return smart.BearerAuthConfig{}, smart.ErrInvalidToken
}

// ResolveBearerTokenCached validates the Bearer token against the matching issuer.
func (m MultiTenantBearerAuth) ResolveBearerTokenCached(ctx context.Context, r *http.Request) (smart.BearerAuthResult, error) {
	cfg, err := m.BearerConfigForRequest(r)
	if err != nil {
		return smart.BearerAuthResult{}, err
	}
	return cfg.ResolveBearerTokenCached(ctx, r)
}

// PrincipalResolver returns an HTTP principal resolver for multi-tenant OAuth tokens.
func (m MultiTenantBearerAuth) PrincipalResolver() hahttp.PrincipalResolver {
	return func(ctx context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error) {
		result, err := m.ResolveBearerTokenCached(ctx, r)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return result.Bundle.Principal, result.Bundle.Tenant, nil
	}
}

// BundleResolver returns an HTTP auth bundle resolver for multi-tenant OAuth tokens.
func (m MultiTenantBearerAuth) BundleResolver() hahttp.AuthBundleResolver {
	return func(ctx context.Context, r *http.Request) (smart.AuthBundle, bool) {
		result, err := m.ResolveBearerTokenCached(ctx, r)
		if err != nil {
			return smart.AuthBundle{}, false
		}
		return result.Bundle, true
	}
}

func bearerTokenFromRequest(r *http.Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("%w: request is nil", ErrInvalidConfig)
	}
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", smart.ErrUnauthorized
	}
	const prefix = "Bearer "
	if len(authHeader) < len(prefix) || authHeader[:len(prefix)] != prefix {
		return "", smart.ErrUnauthorized
	}
	token := authHeader[len(prefix):]
	if token == "" {
		return "", smart.ErrUnauthorized
	}
	return token, nil
}
