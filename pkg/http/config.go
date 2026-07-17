package http

import (
	"context"
	"fmt"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/auth"
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
}

// NewHandler constructs a FHIR REST http.Handler from Config.
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.ResourceService == nil {
		return nil, fmt.Errorf("http: ResourceService is required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = defaultBasePath
	}
	if cfg.Codec == nil {
		cfg.Codec = types.NewJSONCodec()
	}

	h := &handler{cfg: cfg}
	var handler http.Handler = h
	if cfg.AuthMiddleware != nil {
		handler = cfg.AuthMiddleware(handler)
	} else if cfg.PrincipalResolver != nil && cfg.AuthChecker != nil {
		handler = withAuth(handler, cfg.PrincipalResolver, cfg.AuthChecker)
	}
	return handler, nil
}
