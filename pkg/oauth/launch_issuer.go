package oauth

import (
	"crypto/x509"
	"net/http"
	"strings"
	"sync"
)

// LaunchIssuerCredential identifies one EHR or trusted launcher allowed to POST /oauth/launch.
type LaunchIssuerCredential struct {
	ClientID     string
	ClientSecret string
}

// LaunchIssuerRegistry holds launch issuer credentials with optional rotation support.
// Multiple secrets may be registered for the same ClientID during credential rotation.
type LaunchIssuerRegistry struct {
	mu          sync.RWMutex
	credentials map[string][]string // client id -> valid secrets (most recent first)
}

// NewLaunchIssuerRegistry returns an empty launch issuer registry.
func NewLaunchIssuerRegistry() *LaunchIssuerRegistry {
	return &LaunchIssuerRegistry{credentials: make(map[string][]string)}
}

// Register adds or replaces the single secret for a launch issuer id.
func (r *LaunchIssuerRegistry) Register(id, secret string) error {
	if r == nil {
		return ErrInvalidConfig
	}
	id = strings.TrimSpace(id)
	secret = strings.TrimSpace(secret)
	if id == "" || secret == "" {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.credentials == nil {
		r.credentials = make(map[string][]string)
	}
	r.credentials[id] = []string{secret}
	return nil
}

// RegisterRotating registers multiple valid secrets for one launch issuer id.
// Use during credential rotation so both old and new secrets are accepted.
func (r *LaunchIssuerRegistry) RegisterRotating(id string, secrets ...string) error {
	if r == nil {
		return ErrInvalidConfig
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrInvalidConfig
	}
	seen := make(map[string]struct{}, len(secrets))
	unique := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		if _, ok := seen[secret]; ok {
			continue
		}
		seen[secret] = struct{}{}
		unique = append(unique, secret)
	}
	if len(unique) == 0 {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.credentials == nil {
		r.credentials = make(map[string][]string)
	}
	r.credentials[id] = unique
	return nil
}

// Validate reports whether id and secret match a registered launch issuer.
func (r *LaunchIssuerRegistry) Validate(id, secret string) bool {
	if r == nil {
		return false
	}
	id = strings.TrimSpace(id)
	secret = strings.TrimSpace(secret)
	if id == "" || secret == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, candidate := range r.credentials[id] {
		if secret == candidate {
			return true
		}
	}
	return false
}

// LaunchIssuerMTLSConfig optionally requires verified client certificates for /oauth/launch.
type LaunchIssuerMTLSConfig struct {
	// RequireMTLS rejects launch requests without a verified client certificate.
	RequireMTLS bool
	// AllowedCommonNames lists acceptable TLS client certificate subject common names.
	AllowedCommonNames []string
}

func (s *Server) validateLaunchIssuerMTLS(r *http.Request) error {
	cfg := s.cfg.LaunchIssuerMTLS
	if cfg == nil {
		return nil
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		if cfg.RequireMTLS {
			return ErrInvalidClient
		}
		return nil
	}
	if len(cfg.AllowedCommonNames) == 0 {
		return nil
	}
	cert := r.TLS.PeerCertificates[0]
	cn := peerCertificateCommonName(cert)
	for _, allowed := range cfg.AllowedCommonNames {
		if strings.EqualFold(strings.TrimSpace(allowed), cn) {
			return nil
		}
	}
	return ErrInvalidClient
}

func peerCertificateCommonName(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	if cn := strings.TrimSpace(cert.Subject.CommonName); cn != "" {
		return cn
	}
	return strings.TrimSpace(cert.Subject.String())
}
