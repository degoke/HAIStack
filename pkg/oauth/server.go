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
	Issuer          string
	FHIRAudience    string
	SigningKey      *KeySet
	Clients         *ClientStore
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AuthCodeTTL     time.Duration
	Now             func() time.Time
}

// Server is a SMART-compatible OAuth2/OIDC authorization server.
type Server struct {
	cfg         Config
	tokens      *TokenStore
	replayStore smart.ReplayStore
	backendAuth *smart.BackendServiceAuth
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
	backendAuth, err := smart.NewBackendServiceAuthWithStores(
		cfg.Issuer+"/oauth/token",
		clientStoreBridge{store: cfg.Clients},
		smart.NewMemoryReplayStore(),
	)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:         cfg,
		tokens:      NewTokenStore(),
		replayStore: backendAuth.Replay,
		backendAuth: backendAuth,
	}, nil
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
func (s *Server) RegisterClient(client Client) {
	if s == nil || s.cfg.Clients == nil {
		return
	}
	s.cfg.Clients.Register(client)
}

// BearerAuthConfig returns SMART bearer validation wired to this server's signing key.
func (s *Server) BearerAuthConfig(adapter *smart.AuthAdapter) smart.BearerAuthConfig {
	pem, err := s.cfg.SigningKey.PublicKeyPEM()
	if err != nil {
		panic(err)
	}
	verifier := smart.PEMVerifier{
		PublicKeyPEM: pem,
		Algorithm:    s.cfg.SigningKey.Algorithm,
	}
	return smart.BearerAuthConfig{
		Validator: &smart.TokenValidator{Verifier: verifier},
		Adapter:   adapter,
		Options: smart.TokenValidateOptions{
			ExpectedIssuer:   s.cfg.Issuer,
			ExpectedAudience: s.cfg.FHIRAudience,
			RequireIssuer:    true,
			RequireAudience:  true,
			RequireExpiry:    true,
			RequireSubject:   true,
			Now:              s.cfg.Now,
		},
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
	mux.HandleFunc("/oauth/token", s.handleToken)
	mux.HandleFunc("/oauth/jwks", s.handleJWKS)
	mux.HandleFunc("/oauth/register", s.handleRegister)
	return mux
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64URLEncode(b)
}
