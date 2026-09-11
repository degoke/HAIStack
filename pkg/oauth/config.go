package oauth

import (
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// LaunchIssuerAuth identifies the EHR or trusted launcher that may issue SMART launch tokens.
type LaunchIssuerAuth struct {
	ClientID     string
	ClientSecret string
}

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
	// ClientStore optionally persists registered clients (SQLite or dynamic registration).
	// When nil, Clients is used as an in-memory store.
	ClientStore ClientStore
	// ConsentSessionStore persists interactive consent sessions. Defaults to in-memory.
	ConsentSessionStore ConsentSessionStore
	// DynamicClientRegistration enables POST /oauth/register. Defaults to true.
	DynamicClientRegistration *bool
	// RegistrationAccessToken requires Authorization: Bearer <token> on POST /oauth/register when set.
	RegistrationAccessToken string
	// BackendAuth validates client_credentials assertions when configured.
	BackendAuth *smart.BackendServiceAuth
	// LaunchStore stores single-use EHR launch tokens. Defaults to in-memory when nil.
	LaunchStore LaunchStore
	// CodeStore stores authorization codes. Defaults to in-memory for zero-config demos;
	// call store.ApplySQLiteStores before NewServer for production-like hosts.
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
	// ConsentUI customizes the interactive consent page (title, logo, theming, session cookie).
	ConsentUI ConsentUIConfig
	// ConsentLogin optionally authenticates the resource owner before consent is shown.
	ConsentLogin ConsentLoginHandler
	// LaunchIssuerAuth holds a single EHR credential pair for POST /oauth/launch.
	// Prefer LaunchIssuers when rotating credentials.
	LaunchIssuerAuth *LaunchIssuerAuth
	// LaunchIssuers registers one or more launch issuer credentials (supports rotation).
	LaunchIssuers *LaunchIssuerRegistry
	// LaunchIssuerMTLS optionally requires verified client certificates for /oauth/launch.
	LaunchIssuerMTLS *LaunchIssuerMTLSConfig
	// ScopesSupported is advertised in SMART configuration.
	ScopesSupported []string
	// RegisteredClientScopes caps dynamically registered client scopes. When empty,
	// DefaultRegisteredClientScopes is used as both the default and maximum allow-list.
	RegisteredClientScopes []string
	// RequirePKCEForAllClients requires PKCE for confidential clients when true.
	RequirePKCEForAllClients *bool
	// JWKSSigners publishes additional verification keys in JWKS. Defaults to Signer only.
	JWKSSigners []TokenSigner
	// RateLimit configures OAuth endpoint rate limits.
	RateLimit RateLimitConfig
	// Now overrides time.Now for tests.
	Now func() time.Time
}

// Server is a SMART-compatible OAuth 2.0 authorization server.
type Server struct {
	cfg Config

	clients              ClientStore
	codes                AuthorizationCodeStore
	launches             LaunchStore
	refresh              RefreshTokenStore
	revocation           TokenRevocationStore
	consentSessions      ConsentSessionStore
	consentLogin         ConsentLoginHandler
	backendAuth          *smart.BackendServiceAuth
	signer               TokenSigner
	jwksSigners          []TokenSigner
	requirePKCEForAll    bool
	tokenLimiter         *oauthRateLimiter
	registerLimiter      *oauthRateLimiter
	issuer               string
	fhirBase             string
	tokenTTL             time.Duration
	refreshTTL           time.Duration
	rotateRefresh        bool
	autoApprove          bool
	scopes               []string
	nowFn                func() time.Time
	launchIssuerAuthHash string
	launchIssuerAuthID   string
}

// NewServer validates config and returns a Server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Signer == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Clients == nil && cfg.ClientStore == nil {
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
	clients := cfg.ClientStore
	if clients == nil {
		clients = registryClientStore{reg: cfg.Clients}
	}
	consentSessions := cfg.ConsentSessionStore
	if consentSessions == nil {
		consentSessions = NewMemoryConsentSessionStore(nowFn)
	}
	rotateRefresh := true
	if cfg.RotateRefreshTokens != nil {
		rotateRefresh = *cfg.RotateRefreshTokens
	}
	launchAuthHash := ""
	launchAuthID := ""
	if cfg.LaunchIssuerAuth != nil {
		hash, err := hashClientSecret(cfg.LaunchIssuerAuth.ClientSecret)
		if err != nil {
			return nil, err
		}
		launchAuthHash = hash
		launchAuthID = strings.TrimSpace(cfg.LaunchIssuerAuth.ClientID)
	}
	requirePKCE := false
	if cfg.RequirePKCEForAllClients != nil {
		requirePKCE = *cfg.RequirePKCEForAllClients
	}
	jwksSigners := cfg.JWKSSigners
	if len(jwksSigners) == 0 {
		jwksSigners = []TokenSigner{cfg.Signer}
	}
	srv := &Server{
		cfg:                  cfg,
		clients:              clients,
		codes:                codes,
		launches:             launches,
		refresh:              refresh,
		revocation:           revocation,
		consentSessions:      consentSessions,
		consentLogin:         cfg.ConsentLogin,
		backendAuth:          cfg.BackendAuth,
		signer:               cfg.Signer,
		jwksSigners:          jwksSigners,
		requirePKCEForAll:    requirePKCE,
		issuer:               trimSlash(cfg.Issuer),
		fhirBase:             trimSlash(cfg.FHIRBaseURL),
		tokenTTL:             ttl,
		refreshTTL:           refreshTTL,
		rotateRefresh:        rotateRefresh,
		autoApprove:          autoApprove,
		scopes:               scopes,
		nowFn:                nowFn,
		launchIssuerAuthHash: launchAuthHash,
		launchIssuerAuthID:   launchAuthID,
	}
	applyRateLimitConfig(srv, cfg.RateLimit)
	return srv, nil
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

// LookupClient returns a registered client for this issuer.
func (s *Server) LookupClient(clientID string) (Client, error) {
	if s == nil {
		return Client{}, ErrInvalidConfig
	}
	return s.clients.Lookup(s.issuer, clientID)
}

func trimSlash(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
