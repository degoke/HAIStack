package oauth

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Config configures a built-in OAuth2/OIDC authorization server.
type Config struct {
	Issuer             string
	FHIRAudience       string
	SigningKey         *KeySet
	Clients            ClientRegistry
	AuthorizationStore AuthorizationStore
	ReplayStore        smart.ReplayStore
	RevocationStore    TokenRevocationStore
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	AuthCodeTTL        time.Duration
	Now                func() time.Time
	// ConsentHandler approves authorization requests. When nil and AutoApprove is false,
	// the authorize endpoint rejects requests.
	ConsentHandler ConsentHandler
	// AutoApprove issues authorization codes without consent review. Use only in tests.
	AutoApprove bool
	// RequireConsentForm redirects to a built-in HTML consent page before issuing codes.
	RequireConsentForm bool
	// AllowDynamicRegistration enables POST /oauth/register. Disabled by default.
	AllowDynamicRegistration bool
	// RegistrationAccessToken protects POST /oauth/register when set.
	RegistrationAccessToken string
	// LaunchResolver resolves EHR launch tokens for /oauth/launch and authorize.
	LaunchResolver LaunchResolver
	// UserAuthenticator identifies the end user approving access in production flows.
	UserAuthenticator UserAuthenticator
	// LoginPath redirects unauthenticated authorize requests when UserAuthenticator is set.
	LoginPath string
	// RateLimit configures token and registration endpoint rate limits.
	RateLimit RateLimitConfig
	// TokenRateLimiter overrides the default in-memory token endpoint limiter.
	TokenRateLimiter RateLimitStore
	// RegisterRateLimiter overrides the default in-memory registration limiter.
	RegisterRateLimiter RateLimitStore
	// VerificationKeys are additional public keys exposed via JWKS (for rotation).
	VerificationKeys []*KeySet
	// RequirePKCEForAllClients requires code_challenge for every client at authorize.
	RequirePKCEForAllClients bool
	// RegisteredClientScopes limits scopes for dynamic client registration.
	RegisteredClientScopes []string
}

// Server is a SMART-compatible OAuth2/OIDC authorization server.
type Server struct {
	cfg             Config
	authStore       AuthorizationStore
	replayStore     smart.ReplayStore
	revocationStore TokenRevocationStore
	backendAuth     *smart.BackendServiceAuth
	tokenLimiter    RateLimitStore
	registerLimiter RateLimitStore
	loginLimiter    RateLimitStore
}

// NewServer constructs an authorization server.
func NewServer(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, fmt.Errorf("oauth: issuer is required")
	}
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if cfg.SigningKey == nil {
		key, err := NewKeySet(2048)
		if err != nil {
			return nil, err
		}
		cfg.SigningKey = key
	}
	if cfg.Clients == nil {
		cfg.Clients = NewClientStore()
	}
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = time.Hour
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 24 * time.Hour
	}
	if cfg.AuthCodeTTL <= 0 {
		cfg.AuthCodeTTL = 5 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.FHIRAudience == "" {
		cfg.FHIRAudience = cfg.Issuer
	}
	if cfg.AuthorizationStore == nil {
		store := NewMemoryAuthorizationStore()
		store.Now = cfg.Now
		cfg.AuthorizationStore = store
	}
	if cfg.RevocationStore == nil {
		cfg.RevocationStore = NewMemoryTokenRevocationStore()
	}
	replayStore := cfg.ReplayStore
	if replayStore == nil {
		replayStore = smart.NewMemoryReplayStore()
	}
	backendAuth, err := smart.NewBackendServiceAuthWithStores(
		cfg.Issuer+"/oauth/token",
		clientStoreBridge{store: cfg.Clients},
		replayStore,
	)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:             cfg,
		authStore:       cfg.AuthorizationStore,
		replayStore:     replayStore,
		revocationStore: cfg.RevocationStore,
		backendAuth:     backendAuth,
	}
	applyRateLimitConfig(srv, cfg.RateLimit, cfg.TokenRateLimiter, cfg.RegisterRateLimiter)
	return srv, nil
}

// Issuer returns the configured issuer URL.
func (s *Server) Issuer() string {
	if s == nil {
		return ""
	}
	return s.cfg.Issuer
}

// SigningPrivateKey returns the RSA private key used to sign access tokens.
func (s *Server) SigningPrivateKey() *rsa.PrivateKey {
	if s == nil || s.cfg.SigningKey == nil {
		return nil
	}
	return s.cfg.SigningKey.PrivateKey
}

// RegisterClient registers an OAuth client.
func (s *Server) RegisterClient(client Client) error {
	if s == nil || s.cfg.Clients == nil {
		return nil
	}
	return s.cfg.Clients.Register(client)
}

func (s *Server) authenticatedUser(r *http.Request) (UserIdentity, bool) {
	if s == nil || s.cfg.UserAuthenticator == nil {
		return UserIdentity{}, false
	}
	return s.cfg.UserAuthenticator.AuthenticateUser(r)
}

func (s *Server) tokenVerifier() smart.SignatureVerifier {
	var verifiers []smart.SignatureVerifier
	for _, keySet := range s.verificationKeySets() {
		if keySet == nil {
			continue
		}
		pem, err := keySet.PublicKeyPEM()
		if err != nil {
			continue
		}
		verifiers = append(verifiers, smart.PEMVerifier{
			PublicKeyPEM: pem,
			Algorithm:    keySet.Algorithm,
		})
	}
	if len(verifiers) == 0 {
		panic("oauth: no verification keys")
	}
	if len(verifiers) == 1 {
		return verifiers[0]
	}
	return multiSignatureVerifier(verifiers)
}

type multiSignatureVerifier []smart.SignatureVerifier

func (m multiSignatureVerifier) Verify(headerSegment, payloadSegment string, signature []byte, alg string) error {
	var last error
	for _, verifier := range m {
		if verifier == nil {
			continue
		}
		if err := verifier.Verify(headerSegment, payloadSegment, signature, alg); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("oauth: signature verification failed")
}

// BearerAuthConfig returns SMART bearer validation wired to this server's signing and verification keys.
func (s *Server) BearerAuthConfig(adapter *smart.AuthAdapter) smart.BearerAuthConfig {
	opts := smart.TokenValidateOptions{
		ExpectedIssuer:   s.cfg.Issuer,
		ExpectedAudience: s.cfg.FHIRAudience,
		RequireIssuer:    true,
		RequireAudience:  true,
		RequireExpiry:    true,
		RequireSubject:   true,
		Now:              s.cfg.Now,
	}
	if s.revocationStore != nil {
		opts.IsJWTRevoked = s.revocationStore.IsRevoked
	}
	return smart.BearerAuthConfig{
		Validator: &smart.TokenValidator{Verifier: s.tokenVerifier()},
		Adapter:   adapter,
		Options:   opts,
	}
}

// BackendAssertionAuthConfig validates backend client assertions for registered clients.
func (s *Server) BackendAssertionAuthConfig(adapter *smart.AuthAdapter) smart.BackendAssertionAuthConfig {
	return smart.BackendAssertionAuthConfig{
		Backend: s.backendAuth,
		Adapter: adapter,
		Options: smart.TokenValidateOptions{
			RequireIssuer:   true,
			RequireExpiry:   true,
			RequireSubject:  true,
			RequireJWTID:    true,
			RequireAudience: true,
			Now:             s.cfg.Now,
		},
	}
}

// Handler mounts OAuth/OIDC discovery and protocol endpoints on the issuer prefix.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleOpenIDConfiguration)
	mux.HandleFunc("/.well-known/smart-configuration", s.handleSMARTConfiguration)
	mux.HandleFunc("/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("/oauth/token", s.wrapTokenRateLimit(s.handleToken))
	mux.HandleFunc("/oauth/revoke", s.handleRevoke)
	mux.HandleFunc("/oauth/introspect", s.handleIntrospect)
	mux.HandleFunc("/oauth/jwks", s.handleJWKS)
	if s.cfg.AllowDynamicRegistration {
		mux.HandleFunc("/oauth/register", s.wrapRegisterRateLimit(s.handleRegister))
	} else {
		mux.HandleFunc("/oauth/register", s.handleRegister)
	}
	mux.HandleFunc("/oauth/consent", s.handleConsent)
	mux.HandleFunc("/oauth/launch", s.handleLaunch)
	mux.HandleFunc("/oauth/launch/ui", s.handleLaunchUI)
	if s.cfg.LoginPath != "" {
		mux.HandleFunc(s.cfg.LoginPath, s.handleSessionLogin)
	}
	return mux
}

func (s *Server) wrapTokenRateLimit(next http.HandlerFunc) http.HandlerFunc {
	tokenLimit, _, _, window := rateLimitConfigFrom(s.cfg.RateLimit)
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimitOAuth(w, r, "token", s.tokenRateLimiter(tokenLimit, window), tokenLimit, window) {
			return
		}
		next(w, r)
	}
}

func (s *Server) wrapRegisterRateLimit(next http.HandlerFunc) http.HandlerFunc {
	_, registerLimit, _, window := rateLimitConfigFrom(s.cfg.RateLimit)
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimitOAuth(w, r, "register", s.registerRateLimiter(registerLimit, window), registerLimit, window) {
			return
		}
		next(w, r)
	}
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64URLEncode(b)
}
