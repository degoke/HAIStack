package oauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// TenantIssuerConfig describes OAuth issuer settings for one tenant namespace.
type TenantIssuerConfig struct {
	TenantID          string
	Issuer            string
	FHIRAudience      string
	SigningKey        *KeySet
	Clients           ClientRegistry
	AutoApprove       *bool
	ConsentHandler    ConsentHandler
	UserAuthenticator UserAuthenticator
	LoginPath         string
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
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	cfg.FHIRAudience = strings.TrimRight(strings.TrimSpace(cfg.FHIRAudience), "/")
	if cfg.TenantID == "" {
		return fmt.Errorf("%w: tenant id required", ErrInvalidConfig)
	}
	if cfg.Issuer == "" || cfg.FHIRAudience == "" {
		return fmt.Errorf("%w: tenant %q requires issuer and FHIR audience", ErrInvalidConfig, cfg.TenantID)
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
	cfg, ok := r.tenants[strings.TrimSpace(tenantID)]
	r.mu.RUnlock()
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
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	r.mu.RLock()
	id, ok := r.byIssuer[issuer]
	r.mu.RUnlock()
	if !ok {
		return TenantIssuerConfig{}, fmt.Errorf("%w: issuer %q not found", ErrInvalidConfig, issuer)
	}
	return r.Lookup(id)
}

// IDs returns registered tenant ids.
func (r *TenantRegistry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]string, 0, len(r.tenants))
	for id := range r.tenants {
		out = append(out, id)
	}
	r.mu.RUnlock()
	return out
}

// MultiTenantConfig configures a shared OAuth backend with per-tenant issuers.
type MultiTenantConfig struct {
	Base    Config
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
	if cfg.Base.SigningKey == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Base.Clients == nil {
		cfg.Base.Clients = NewClientStore()
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
	cfg.FHIRAudience = tenant.FHIRAudience
	if tenant.SigningKey != nil {
		cfg.SigningKey = tenant.SigningKey
	}
	if tenant.Clients != nil {
		cfg.Clients = tenant.Clients
	}
	if tenant.ConsentHandler != nil {
		cfg.ConsentHandler = tenant.ConsentHandler
	}
	if tenant.UserAuthenticator != nil {
		cfg.UserAuthenticator = tenant.UserAuthenticator
	}
	if tenant.LoginPath != "" {
		cfg.LoginPath = tenant.LoginPath
	}
	if tenant.AutoApprove != nil {
		cfg.AutoApprove = *tenant.AutoApprove
	}
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

// CombineHandlers mounts multiple OAuth handlers on one mux by path prefix.
func CombineHandlers(primary http.Handler, extras ...http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/t/") {
			for _, extra := range extras {
				if extra != nil {
					extra.ServeHTTP(w, r)
					return
				}
			}
		}
		if primary != nil {
			primary.ServeHTTP(w, r)
			return
		}
		for _, extra := range extras {
			if extra != nil {
				extra.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
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
