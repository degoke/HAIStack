package http

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const defaultBasePath = "/fhir"

// ServerMetadata supplies static server fields for CapabilityStatement generation.
type ServerMetadata struct {
	SoftwareName    string
	SoftwareVersion string
	ServerName      string
	Description     string
}

// PrincipalResolver extracts the authenticated principal and tenant from a request.
type PrincipalResolver func(ctx context.Context, r *http.Request) (auth.Principal, auth.TenantContext, error)

// AuthBundleResolver optionally supplies a validated SMART AuthBundle for scope-filter enforcement.
type AuthBundleResolver func(ctx context.Context, r *http.Request) (smart.AuthBundle, bool)

// AuthChecker authorizes FHIR read, write, and search actions.
type AuthChecker interface {
	AuthorizeRead(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType, id string) (auth.Decision, error)
	AuthorizeWrite(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, operation, resourceType, id string) (auth.Decision, error)
	AuthorizeSearch(ctx context.Context, principal auth.Principal, tenant auth.TenantContext, resourceType string) (auth.Decision, error)
}

// Config configures the FHIR HTTP handler.
type Config struct {
	// BasePath is the FHIR REST root path. Defaults to /fhir.
	BasePath string

	// ResourceService is required.
	ResourceService ResourceService

	// SearchService is optional; when nil, type-level GET search is unavailable.
	SearchService SearchService

	// SDCService is optional; when nil, SDC operation endpoints return
	// OperationOutcome with a not-supported error.
	SDCService SDCService

	// PackageInstallService handles ImplementationGuide/$install.
	// When nil, POST /fhir/ImplementationGuide/$install returns not-supported.
	PackageInstallService PackageInstallService

	// OperationService handles non-SDC custom operations such as
	// $everything or implementation-specific operations.
	OperationService OperationService

	// ValidateService handles FHIR Resource/$validate for non-SDC resource types.
	// When nil, POST /fhir/{type}/$validate returns not-supported.
	ValidateService ValidateService

	// CapabilitySource is optional; when nil, /metadata returns not-supported.
	CapabilitySource CapabilitySource

	// ServerMetadata enriches generated CapabilityStatement resources.
	ServerMetadata ServerMetadata

	// Codec parses and serializes FHIR JSON. Defaults to types.NewJSONCodec().
	Codec types.ResourceCodec

	// AuthMiddleware wraps the handler when set. When nil and PrincipalResolver
	// plus AuthChecker are configured, built-in auth middleware is used.
	AuthMiddleware func(http.Handler) http.Handler

	// PrincipalResolver extracts request identity when auth is enabled.
	PrincipalResolver PrincipalResolver

	// AuthChecker authorizes actions when auth is enabled.
	AuthChecker AuthChecker

	// AuthBundleResolver stores a SMART AuthBundle on the request context for
	// granular scope filter enforcement. Optional when hosts do not use SMART 2.2 filters.
	AuthBundleResolver AuthBundleResolver

	// ScopeFilterMatcher evaluates SMART 2.2 scope filters for this handler. When nil,
	// the package default from smart.SetScopeFilterMatcher is used.
	ScopeFilterMatcher smart.ScopeFilterMatcher

	// PatientReferenceResolver resolves patient ownership for loaded resources when
	// TenantContext.PatientScope is set. Required for patient-scoped read/search enforcement.
	PatientReferenceResolver auth.ResourcePatientResolver

	// RateLimit enables process-local request limiting when Requests and Window
	// are configured. Use a distributed gateway limiter for multi-instance
	// deployments, or provide equivalent protection before this handler.
	RateLimit RateLimitConfig
}

// NewHandler constructs a FHIR REST http.Handler from Config.
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.ResourceService == nil {
		return nil, fmt.Errorf("http: ResourceService is required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = defaultBasePath
	}
	cfg.BasePath = strings.TrimSpace(cfg.BasePath)
	if !strings.HasPrefix(cfg.BasePath, "/") {
		cfg.BasePath = "/" + cfg.BasePath
	}
	cfg.BasePath = strings.TrimSuffix(cfg.BasePath, "/")
	if cfg.BasePath == "" {
		cfg.BasePath = "/"
	}
	if cfg.Codec == nil {
		cfg.Codec = types.NewJSONCodec()
	}

	h := &handler{cfg: cfg}
	var handler http.Handler = h
	if cfg.ScopeFilterMatcher != nil {
		handler = withScopeFilterMatcher(handler, cfg.ScopeFilterMatcher)
	}
	if cfg.AuthMiddleware != nil {
		handler = cfg.AuthMiddleware(handler)
	} else if cfg.PrincipalResolver != nil && cfg.AuthChecker != nil {
		handler = withAuth(handler, cfg.PrincipalResolver, cfg.AuthChecker, cfg.AuthBundleResolver)
	}
	if cfg.RateLimit.Requests > 0 && cfg.RateLimit.Window > 0 {
		handler = NewRateLimitMiddleware(cfg.RateLimit)(handler)
	}
	return handler, nil
}
