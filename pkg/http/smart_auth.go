package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// SMARTBearerAuthMiddleware validates Bearer JWTs and stores principal, tenant, and AuthBundle on the request context.
func SMARTBearerAuthMiddleware(cfg smart.BearerAuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			format, err := negotiateResponseFormat(r)
			if err != nil {
				writeError(w, err)
				return
			}
			w = withResponseFormat(w, format)
			result, err := cfg.ResolveBearerToken(r)
			if err != nil {
				writeError(w, mapAuthResolverError(err))
				return
			}
			ctx := smart.ContextWithAuthBundle(r.Context(), result.Bundle)
			ctx = contextWithIdentity(ctx, result.Bundle.Principal, result.Bundle.Tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SMARTBearerPrincipalResolver returns principal and tenant from a validated Bearer token.
func SMARTBearerPrincipalResolver(cfg smart.BearerAuthConfig) PrincipalResolver {
	return func(_ context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error) {
		result, err := cfg.ResolveBearerToken(r)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return result.Bundle.Principal, result.Bundle.Tenant, nil
	}
}

// SMARTBearerBundleResolver returns the AuthBundle from a validated Bearer token.
func SMARTBearerBundleResolver(cfg smart.BearerAuthConfig) AuthBundleResolver {
	return func(_ context.Context, r *http.Request) (smart.AuthBundle, bool) {
		result, err := cfg.ResolveBearerToken(r)
		if err != nil {
			return smart.AuthBundle{}, false
		}
		return result.Bundle, true
	}
}

func contextWithIdentity(ctx context.Context, principal auth.Principal, tenant auth.TenantContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, requestIdentity{
		Principal: principal,
		Tenant:    tenant,
	})
}

func mapAuthResolverError(err error) error {
	if err == nil {
		return errUnauthenticated
	}
	if errors.Is(err, smart.ErrUnauthorized) ||
		errors.Is(err, smart.ErrTokenExpired) ||
		errors.Is(err, smart.ErrTokenNotYetValid) ||
		errors.Is(err, smart.ErrReplay) ||
		errors.Is(err, smart.ErrInvalidToken) ||
		errors.Is(err, smart.ErrIssuerMismatch) ||
		errors.Is(err, smart.ErrAudienceMismatch) ||
		errors.Is(err, smart.ErrMissingScopes) {
		return err
	}
	return errUnauthenticated
}
