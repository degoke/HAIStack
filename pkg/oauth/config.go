package oauth

import (
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Config configures the built-in OAuth authorization server.
type Config struct {
	// Issuer is the OAuth issuer URL (typically the server root, no trailing slash).
	Issuer string
	// FHIRBaseURL is the audience for access tokens and SMART metadata.
	FHIRBaseURL string
	// TokenTTL sets access token lifetime. Defaults to one hour.
	TokenTTL time.Duration
	// RefreshTokenTTL sets refresh token lifetime. Defaults to 30 days.
	RefreshTokenTTL time.Duration
	// Signer signs issued JWT access tokens (RS256 or HS256).
	Signer TokenSigner
	// Clients is the static client registry. Required.
	Clients *ClientRegistry
	// BackendAuth validates client_credentials assertions when configured.
	BackendAuth *smart.BackendServiceAuth
	// LaunchStore stores single-use EHR launch tokens. Defaults to in-memory when nil.
	LaunchStore LaunchStore
	// CodeStore stores authorization codes. Defaults to in-memory.
	CodeStore AuthorizationCodeStore
	// RefreshStore stores refresh tokens. Defaults to in-memory when nil.
	RefreshStore RefreshTokenStore
	// RevocationStore tracks revoked access token JTIs. Defaults to in-memory when nil.
	RevocationStore TokenRevocationStore
	// RotateRefreshTokens rotates refresh tokens on each use. Defaults to true.
	RotateRefreshTokens *bool
	// AutoApprove skips interactive consent for registered clients (demo/tests only).
	// When nil, defaults to false; production deployments should keep consent enabled.
	AutoApprove *bool
	// ScopesSupported is advertised in SMART configuration.
	ScopesSupported []string
	// Now overrides time.Now for tests.
	Now func() time.Time
}

// Server is a SMART-compatible OAuth 2.0 authorization server.
type Server struct {
	cfg Config

	clients       *ClientRegistry
	codes         AuthorizationCodeStore
	launches      LaunchStore
	refresh       RefreshTokenStore
	revocation    TokenRevocationStore
	backendAuth   *smart.BackendServiceAuth
	signer        TokenSigner
	issuer        string
	fhirBase      string
	tokenTTL      time.Duration
	refreshTTL    time.Duration
	rotateRefresh bool
	autoApprove   bool
	scopes        []string
	nowFn         func() time.Time
}

// NewServer validates config and returns a Server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Signer == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Clients == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Issuer == "" || cfg.FHIRBaseURL == "" {
		return nil, ErrInvalidConfig
	}
	codes := cfg.CodeStore
	if codes == nil {
		codes = NewCodeStore()
	}
	refresh := cfg.RefreshStore
	if refresh == nil {
		refresh = NewMemoryRefreshStore()
	}
	revocation := cfg.RevocationStore
	if revocation == nil {
		revocation = NewMemoryRevocationStore()
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	if mem, ok := codes.(*CodeStore); ok && mem.Now == nil {
		mem.Now = nowFn
	}
	if mem, ok := refresh.(*MemoryRefreshStore); ok && mem.Now == nil {
		mem.Now = nowFn
	}
	if mem, ok := revocation.(*MemoryRevocationStore); ok && mem.Now == nil {
		mem.Now = nowFn
	}
	launches := cfg.LaunchStore
	if launches == nil {
		launches = NewMemoryLaunchStore()
	}
	if mem, ok := launches.(*MemoryLaunchStore); ok && mem.Now == nil {
		mem.Now = nowFn
	}
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = defaultAccessTokenTTL
	}
	refreshTTL := cfg.RefreshTokenTTL
	if refreshTTL <= 0 {
		refreshTTL = defaultRefreshTokenTTL
	}
	scopes := cfg.ScopesSupported
	if len(scopes) == 0 {
		scopes = []string{
			"openid", "fhirUser", "offline_access", "launch", "launch/patient",
			"patient/*.read", "patient/*.write",
			"user/*.read", "user/*.write",
			"system/*.read", "system/*.write",
		}
	}
	autoApprove := false
	if cfg.AutoApprove != nil {
		autoApprove = *cfg.AutoApprove
	}
	rotateRefresh := true
	if cfg.RotateRefreshTokens != nil {
		rotateRefresh = *cfg.RotateRefreshTokens
	}
	return &Server{
		cfg:           cfg,
		clients:       cfg.Clients,
		codes:         codes,
		launches:      launches,
		refresh:       refresh,
		revocation:    revocation,
		backendAuth:   cfg.BackendAuth,
		signer:        cfg.Signer,
		issuer:        trimSlash(cfg.Issuer),
		fhirBase:      trimSlash(cfg.FHIRBaseURL),
		tokenTTL:      ttl,
		refreshTTL:    refreshTTL,
		rotateRefresh: rotateRefresh,
		autoApprove:   autoApprove,
		scopes:        scopes,
		nowFn:         nowFn,
	}, nil
}

func (s *Server) Issuer() string      { return s.issuer }
func (s *Server) FHIRBaseURL() string { return s.fhirBase }
func (s *Server) TokenEndpoint() string {
	return s.issuer + "/oauth/token"
}
func (s *Server) AuthorizationEndpoint() string {
	return s.issuer + "/oauth/authorize"
}

// Verifier returns the smart.SignatureVerifier for tokens issued by this server.
func (s *Server) Verifier() smart.SignatureVerifier {
	if s == nil || s.signer == nil {
		return nil
	}
	return s.signer.Verifier()
}

// RevocationStore returns the configured token revocation store.
func (s *Server) RevocationStore() TokenRevocationStore {
	if s == nil {
		return nil
	}
	return s.revocation
}

func trimSlash(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
