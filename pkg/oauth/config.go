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
	// Signer signs issued JWT access tokens (RS256 or HS256).
	Signer TokenSigner
	// Clients is the static client registry. Required.
	Clients *ClientRegistry
	// BackendAuth validates client_credentials assertions when configured.
	BackendAuth *smart.BackendServiceAuth
	// CodeStore stores authorization codes. Defaults to in-memory.
	CodeStore *CodeStore
	// AutoApprove enables silent authorization for registered clients (v1 demo default).
	// When nil, defaults to true.
	AutoApprove *bool
	// ScopesSupported is advertised in SMART configuration.
	ScopesSupported []string
	// Now overrides time.Now for tests.
	Now func() time.Time
}

// Server is a SMART-compatible OAuth 2.0 authorization server.
type Server struct {
	cfg Config

	clients     *ClientRegistry
	codes       *CodeStore
	backendAuth *smart.BackendServiceAuth
	signer      TokenSigner
	issuer      string
	fhirBase    string
	tokenTTL    time.Duration
	autoApprove bool
	scopes      []string
	now         func() time.Time
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
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	if codes.Now == nil {
		codes.Now = nowFn
	}
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = defaultAccessTokenTTL
	}
	scopes := cfg.ScopesSupported
	if len(scopes) == 0 {
		scopes = []string{
			"openid", "fhirUser", "launch", "launch/patient",
			"patient/*.read", "patient/*.write",
			"user/*.read", "user/*.write",
			"system/*.read", "system/*.write",
		}
	}
	autoApprove := true
	if cfg.AutoApprove != nil {
		autoApprove = *cfg.AutoApprove
	}
	return &Server{
		cfg:         cfg,
		clients:     cfg.Clients,
		codes:       codes,
		backendAuth: cfg.BackendAuth,
		signer:      cfg.Signer,
		issuer:      trimSlash(cfg.Issuer),
		fhirBase:    trimSlash(cfg.FHIRBaseURL),
		tokenTTL:    ttl,
		autoApprove: autoApprove,
		scopes:      scopes,
		now:         nowFn,
	}, nil
}

func (s *Server) Issuer() string       { return s.issuer }
func (s *Server) FHIRBaseURL() string  { return s.fhirBase }
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

func trimSlash(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
