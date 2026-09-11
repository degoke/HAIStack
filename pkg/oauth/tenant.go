package oauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// TenantIssuerConfig describes OAuth issuer settings for one tenant namespace.
// Multi-tenant servers merge Base with each tenant entry in ServerForTenant.
// Fields below replace (not inherit) the corresponding Base settings for that tenant.
type TenantIssuerConfig struct {
	// TenantID is the stable tenant identifier used in routes and token tenant hints.
	TenantID string
	// Issuer is the OAuth issuer URL for this tenant (no trailing slash).
	Issuer string
	// FHIRBaseURL is the access token audience / SMART FHIR base for this tenant.
	FHIRBaseURL string
	// Signer replaces the base signer for this tenant when non-nil.
	Signer TokenSigner
	// Clients replaces the base client registry for this tenant when non-nil.
	Clients *ClientRegistry
	// ScopesSupported replaces advertised scopes for this tenant when non-empty.
	ScopesSupported []string
	// ConsentUI replaces base consent branding for this tenant when non-nil.
	ConsentUI *ConsentUIConfig
	// ConsentLogin replaces the base consent login handler for this tenant when non-nil.
	ConsentLogin ConsentLoginHandler
	// AutoApprove replaces the base auto-approve setting for this tenant when non-nil.
	AutoApprove *bool
	// LaunchIssuerAuth is the only launch credential source for this tenant (base creds are not inherited).
	LaunchIssuerAuth *LaunchIssuerAuth
	// LaunchIssuers is the only launch credential registry for this tenant (base registries are not inherited).
	LaunchIssuers *LaunchIssuerRegistry
	// LaunchIssuerMTLS is the only launch mTLS configuration for this tenant (base settings are not inherited).
	LaunchIssuerMTLS *LaunchIssuerMTLSConfig
}

// TenantRegistry stores per-tenant issuer configuration.
type TenantRegistry struct {
	mu       sync.RWMutex
	tenants  map[string]TenantIssuerConfig
	byIssuer map[string]string
}

// NewTenantRegistry returns an empty tenant registry.
func NewTenantRegistry() *TenantRegistry {
	return &TenantRegistry{
		tenants:  make(map[string]TenantIssuerConfig),
		byIssuer: make(map[string]string),
	}
}

// Register upserts a tenant issuer configuration.
func (r *TenantRegistry) Register(cfg TenantIssuerConfig) error {
	if r == nil {
		return fmt.Errorf("%w: tenant registry is nil", ErrInvalidConfig)
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.Issuer = trimSlash(cfg.Issuer)
	cfg.FHIRBaseURL = trimSlash(cfg.FHIRBaseURL)
	if cfg.TenantID == "" {
		return fmt.Errorf("%w: tenant id required", ErrInvalidConfig)
	}
	if cfg.Issuer == "" || cfg.FHIRBaseURL == "" {
		return fmt.Errorf("%w: tenant %q requires issuer and fhir base url", ErrInvalidConfig, cfg.TenantID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenants == nil {
		r.tenants = make(map[string]TenantIssuerConfig)
	}
	if r.byIssuer == nil {
		r.byIssuer = make(map[string]string)
	}
	for iss, id := range r.byIssuer {
		if id != cfg.TenantID && iss == cfg.Issuer {
			return fmt.Errorf("%w: issuer %q already registered for tenant %q", ErrInvalidConfig, cfg.Issuer, id)
		}
	}
	r.tenants[cfg.TenantID] = cfg
	r.byIssuer[cfg.Issuer] = cfg.TenantID
	return nil
}

// Lookup returns configuration for a tenant id.
func (r *TenantRegistry) Lookup(tenantID string) (TenantIssuerConfig, error) {
	if r == nil {
		return TenantIssuerConfig{}, fmt.Errorf("%w: tenant %q not found", ErrInvalidConfig, tenantID)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.tenants[strings.TrimSpace(tenantID)]
	if !ok {
		return TenantIssuerConfig{}, fmt.Errorf("%w: tenant %q not found", ErrInvalidConfig, tenantID)
	}
	return cfg, nil
}

// LookupByIssuer returns tenant configuration matching an OAuth issuer URL.
func (r *TenantRegistry) LookupByIssuer(issuer string) (TenantIssuerConfig, error) {
	if r == nil {
		return TenantIssuerConfig{}, fmt.Errorf("%w: issuer not found", ErrInvalidConfig)
	}
	issuer = trimSlash(issuer)
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byIssuer[issuer]
	if !ok {
		return TenantIssuerConfig{}, fmt.Errorf("%w: issuer %q not found", ErrInvalidConfig, issuer)
	}
	return r.tenants[id], nil
}

// IDs returns registered tenant ids in stable order.
func (r *TenantRegistry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tenants))
	for id := range r.tenants {
		out = append(out, id)
	}
	return out
}

// MultiTenantConfig configures a shared OAuth backend with per-tenant issuers.
type MultiTenantConfig struct {
	// Base holds shared stores, signers, and defaults applied to every tenant.
	Base Config
	// Tenants is required and lists per-tenant issuer settings.
	Tenants *TenantRegistry
}

// MultiTenantServer hosts OAuth endpoints for multiple tenant issuers.
type MultiTenantServer struct {
	base    Config
	tenants *TenantRegistry
	servers map[string]*Server
	mu      sync.RWMutex
}

// NewMultiTenantServer validates config and returns a multi-tenant OAuth server.
func NewMultiTenantServer(cfg MultiTenantConfig) (*MultiTenantServer, error) {
	if cfg.Tenants == nil || len(cfg.Tenants.IDs()) == 0 {
		return nil, fmt.Errorf("%w: at least one tenant required", ErrInvalidConfig)
	}
	if cfg.Base.Signer == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Base.Clients == nil {
		cfg.Base.Clients = NewClientRegistry()
	}
	return &MultiTenantServer{
		base:    cfg.Base,
		tenants: cfg.Tenants,
		servers: make(map[string]*Server),
	}, nil
}

// LookupByIssuer returns tenant configuration for a token issuer URL.
func (m *MultiTenantServer) LookupByIssuer(issuer string) (TenantIssuerConfig, error) {
	if m == nil || m.tenants == nil {
		return TenantIssuerConfig{}, ErrInvalidConfig
	}
	return m.tenants.LookupByIssuer(issuer)
}

// ServerForTenant returns a tenant-scoped authorization server view.
func (m *MultiTenantServer) ServerForTenant(tenantID string) (*Server, error) {
	if m == nil {
		return nil, ErrInvalidConfig
	}
	m.mu.RLock()
	if srv, ok := m.servers[tenantID]; ok {
		m.mu.RUnlock()
		return srv, nil
	}
	m.mu.RUnlock()

	tenant, err := m.tenants.Lookup(tenantID)
	if err != nil {
		return nil, err
	}
	cfg := m.base
	cfg.Issuer = tenant.Issuer
	cfg.FHIRBaseURL = tenant.FHIRBaseURL
	if tenant.Signer != nil {
		cfg.Signer = tenant.Signer
	}
	if tenant.Clients != nil {
		cfg.Clients = tenant.Clients
		if cfg.ClientStore != nil {
			cfg.ClientStore = overlayClientStore{overlay: tenant.Clients, base: cfg.ClientStore}
		} else {
			cfg.ClientStore = registryClientStore{reg: tenant.Clients}
		}
	}
	if len(tenant.ScopesSupported) > 0 {
		cfg.ScopesSupported = tenant.ScopesSupported
	}
	if tenant.ConsentUI != nil {
		cfg.ConsentUI = *tenant.ConsentUI
	}
	if tenant.ConsentLogin != nil {
		cfg.ConsentLogin = tenant.ConsentLogin
	}
	if tenant.AutoApprove != nil {
		cfg.AutoApprove = tenant.AutoApprove
	}
	// Multi-tenant servers do not inherit base launch credentials; each tenant must
	// register its own launch issuer settings explicitly.
	cfg.LaunchIssuerAuth = tenant.LaunchIssuerAuth
	cfg.LaunchIssuers = tenant.LaunchIssuers
	cfg.LaunchIssuerMTLS = tenant.LaunchIssuerMTLS
	srv, err := NewServer(cfg)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.servers[tenantID] = srv
	m.mu.Unlock()
	return srv, nil
}

// Handler mounts tenant-scoped routes under /t/{tenantID}/.
func (m *MultiTenantServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, rest, ok := splitTenantPath(r.URL.Path)
		if !ok {
			writeOAuthError(w, http.StatusNotFound, "not_found", "tenant route must begin with /t/{tenantId}/")
			return
		}
		srv, err := m.ServerForTenant(tenantID)
		if err != nil {
			writeOAuthError(w, http.StatusNotFound, "not_found", "unknown tenant")
			return
		}
		r2 := *r
		r2.URL = cloneURL(r.URL)
		r2.URL.Path = rest
		srv.Handler().ServeHTTP(w, &r2)
	})
}

func splitTenantPath(path string) (tenantID, rest string, ok bool) {
	if !strings.HasPrefix(path, "/t/") {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(path, "/t/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	tenantID = parts[0]
	rest = "/"
	if len(parts) == 2 {
		rest = "/" + parts[1]
	}
	return tenantID, rest, true
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{}
	}
	copy := *u
	return &copy
}
