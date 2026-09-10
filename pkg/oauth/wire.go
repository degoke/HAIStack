package oauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// WireConfig configures HTTP integration between pkg/oauth and pkg/http.
type WireConfig struct {
	Server  *Server
	Adapter *smart.AuthAdapter
	// TokenValidateOptions configures inbound access token validation.
	TokenValidateOptions smart.TokenValidateOptions
	// RevocationStore overrides server revocation checks when set.
	RevocationStore TokenRevocationStore
}

// WireResult holds handlers and resolvers produced by WireHTTP.
type WireResult struct {
	// OAuthHandler serves /oauth/* and /.well-known/* routes.
	OAuthHandler http.Handler
	// PrincipalResolver validates Bearer tokens issued by the server.
	PrincipalResolver hahttp.PrincipalResolver
	// Adapter is the resolved SMART auth adapter shared with ScopePolicyAuthChecker.
	Adapter *smart.AuthAdapter
}

// ScopePolicyAuthChecker returns an AuthChecker wired to the same SMART adapter as WireHTTP.
func (r WireResult) ScopePolicyAuthChecker(engine auth.PolicyEngine) ScopePolicyAuthChecker {
	return ScopePolicyAuthChecker{Adapter: r.Adapter, Engine: engine}
}

// WireHTTP builds OAuth routes and a PrincipalResolver for pkg/http.
func WireHTTP(cfg WireConfig) (WireResult, error) {
	if cfg.Server == nil {
		return WireResult{}, fmt.Errorf("%w: server required", ErrInvalidConfig)
	}
	adapter := cfg.Adapter
	if adapter == nil {
		adapter = smart.NewAuthAdapter(smart.AuthAdapterConfig{})
	}
	opts := cfg.TokenValidateOptions
	if opts.ExpectedIssuer == "" {
		opts.ExpectedIssuer = cfg.Server.Issuer()
	}
	if opts.ExpectedAudience == "" {
		opts.ExpectedAudience = cfg.Server.FHIRBaseURL()
	}
	resolver := BearerPrincipalResolver(cfg.Server, adapter, opts, firstNonEmptyRevocation(cfg))
	return WireResult{
		OAuthHandler:      cfg.Server.Handler(),
		PrincipalResolver: resolver,
		Adapter:           adapter,
	}, nil
}

// BearerPrincipalResolver returns a PrincipalResolver that validates Bearer
// tokens issued by the given OAuth server.
func BearerPrincipalResolver(server *Server, adapter *smart.AuthAdapter, opts smart.TokenValidateOptions, revocation TokenRevocationStore) hahttp.PrincipalResolver {
	verifier := server.Verifier()
	validator := smart.NewTokenValidator(verifier)
	if revocation == nil {
		revocation = server.RevocationStore()
	}
	return func(ctx context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error) {
		token, err := bearerToken(r)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		if revocation != nil {
			jti, _, err := ParseAccessTokenJTI(token)
			if err == nil && jti != "" {
				revoked, err := revocation.IsRevoked(jti)
				if err != nil {
					return auth.Principal{}, auth.TenantContext{}, err
				}
				if revoked {
					return auth.Principal{}, auth.TenantContext{}, smart.ErrInvalidToken
				}
			}
		}
		claims, err := validator.ValidateToken(token, opts)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		launch := smart.ExtractLaunchContext(claims, smart.LaunchContextInput{})
		bundle, err := adapter.ToAuthRequests(claims, launch)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		return bundle.Principal, bundle.Tenant, nil
	}
}

// ScopePolicyAuthChecker enforces SMART scopes via AuthAdapter and then pkg/auth policy.
// Policy deny wins when the engine returns ErrDenied or Allowed=false.
// When Engine is nil, access is denied after SMART scope checks pass.
type ScopePolicyAuthChecker struct {
	Adapter *smart.AuthAdapter
	Engine  auth.PolicyEngine
}

func (c ScopePolicyAuthChecker) AuthorizeRead(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType, id string) (auth.Decision, error) {
	return c.authorize(ctx, principal, tenant, "read", resourceType, id)
}

func (c ScopePolicyAuthChecker) AuthorizeWrite(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, operation, resourceType, id string) (auth.Decision, error) {
	return c.authorize(ctx, principal, tenant, operation, resourceType, id)
}

func (c ScopePolicyAuthChecker) AuthorizeSearch(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType string) (auth.Decision, error) {
	return c.authorize(ctx, principal, tenant, "search", resourceType, "")
}

func (c ScopePolicyAuthChecker) authorize(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, action, resourceType, id string) (auth.Decision, error) {
	adapter := c.Adapter
	if adapter == nil {
		adapter = smart.NewAuthAdapter(smart.AuthAdapterConfig{})
	}
	scopeRaw := ""
	if principal.Attributes != nil {
		scopeRaw = principal.Attributes["smart.scope"]
	}
	scopes, err := smart.ParseScopes(scopeRaw)
	if err != nil || scopes.Empty() {
		return auth.Decision{Allowed: false, Reason: "missing SMART scopes"}, nil
	}
	bundle := smart.AuthBundle{Principal: principal, Tenant: tenant, Scopes: scopes}
	verb := smart.VerbRead
	if action != "read" && action != "search" {
		verb = smart.VerbWrite
	}
	if !adapter.ScopeImplies(bundle, resourceType, verb) {
		return auth.Decision{Allowed: false, Reason: "SMART scope does not authorize " + action + " on " + resourceType}, nil
	}
	if c.Engine == nil {
		return auth.Decision{Allowed: false, Reason: "authorization policy engine not configured"}, nil
	}
	switch action {
	case "read", "search":
		decision, err := c.Engine.CanReadResource(ctx, auth.ReadRequest{
			Principal:    principal,
			Tenant:       tenant,
			ResourceType: resourceType,
			ID:           id,
		})
		if err != nil {
			return auth.Decision{}, err
		}
		return decision, nil
	default:
		decision, err := c.Engine.CanWriteResource(ctx, auth.WriteRequest{
			Principal:    principal,
			Tenant:       tenant,
			Operation:    action,
			ResourceType: resourceType,
			ID:           id,
		})
		if err != nil {
			return auth.Decision{}, err
		}
		return decision, nil
	}
}

func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", fmt.Errorf("missing authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", fmt.Errorf("authorization header must use Bearer scheme")
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func firstNonEmptyRevocation(cfg WireConfig) TokenRevocationStore {
	if cfg.RevocationStore != nil {
		return cfg.RevocationStore
	}
	if cfg.Server != nil {
		return cfg.Server.RevocationStore()
	}
	return nil
}

// MountRootHandler combines FHIR, optional sync, and OAuth routes on one mux.
func MountRootHandler(fhir http.Handler, oauth http.Handler, sync hahttp.SyncHubServer, syncMiddleware func(http.Handler) http.Handler) http.Handler {
	cfg := hahttp.RootConfig{FHIR: fhir, Sync: sync, SyncMiddleware: syncMiddleware, OAuth: oauth}
	return hahttp.NewRootHandlerFromConfig(cfg)
}

// WireMultiTenantHTTP builds a tenant-scoped OAuth handler and issuer-aware resolver.
func WireMultiTenantHTTP(cfg MultiTenantConfig, adapter *smart.AuthAdapter) (WireResult, error) {
	mts, err := NewMultiTenantServer(cfg)
	if err != nil {
		return WireResult{}, err
	}
	if adapter == nil {
		adapter = smart.NewAuthAdapter(smart.AuthAdapterConfig{})
	}
	resolver := BearerPrincipalResolverMultiTenant(mts, adapter)
	return WireResult{
		OAuthHandler:      mts.Handler(),
		PrincipalResolver: resolver,
		Adapter:           adapter,
	}, nil
}

// BearerPrincipalResolverMultiTenant validates Bearer tokens against the matching tenant issuer.
func BearerPrincipalResolverMultiTenant(mts *MultiTenantServer, adapter *smart.AuthAdapter) hahttp.PrincipalResolver {
	return func(ctx context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error) {
		token, err := bearerToken(r)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		unverified, err := smart.ParseTokenUnverified(token)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		tenantCfg, err := mts.LookupByIssuer(unverified.Issuer)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		srv, err := mts.ServerForTenant(tenantCfg.TenantID)
		if err != nil {
			return auth.Principal{}, auth.TenantContext{}, err
		}
		opts := smart.TokenValidateOptions{
			ExpectedIssuer:   tenantCfg.Issuer,
			ExpectedAudience: tenantCfg.FHIRBaseURL,
		}
		resolver := BearerPrincipalResolver(srv, adapter, opts, srv.RevocationStore())
		return resolver(ctx, r)
	}
}
