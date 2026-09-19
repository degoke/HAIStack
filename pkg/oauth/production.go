package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SigningKeyPaths names the PEM signing-key fallback on disk.
// Token, client, replay, and revocation state use pkg/oauth/store (SQLite or Postgres).
type SigningKeyPaths struct {
	StateDir   string
	SigningKey string
	SigningKID string
}

// DefaultSigningKeyPaths returns the conventional PEM path under stateDir.
func DefaultSigningKeyPaths(stateDir string) SigningKeyPaths {
	stateDir = strings.TrimSpace(stateDir)
	return SigningKeyPaths{
		StateDir:   stateDir,
		SigningKey: filepath.Join(stateDir, "oauth-signing.pem"),
	}
}

// ApplyProductionDefaults applies safer OAuth defaults for production-like hosts.
// Call after store wiring and before NewServer.
func ApplyProductionDefaults(cfg *Config) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	if err := ValidateProductionIssuer(cfg.Issuer); err != nil {
		return err
	}
	if cfg.AutoApprove {
		return fmt.Errorf("%w: AutoApprove must be false for production", ErrInvalidConfig)
	}
	if err := RequireSigningKeyEncryptionSecret(); err != nil {
		return err
	}
	cfg.RequirePKCEForAllClients = true
	if cfg.AllowDynamicRegistration && strings.TrimSpace(cfg.RegistrationAccessToken) == "" {
		return fmt.Errorf("%w: set RegistrationAccessToken or disable dynamic client registration for production", ErrInvalidConfig)
	}
	return nil
}

// ValidateProductionIssuer requires a non-empty https issuer URL.
func ValidateProductionIssuer(issuer string) error {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return fmt.Errorf("oauth: issuer URL is required for production")
	}
	u, err := url.Parse(issuer)
	if err != nil || strings.ToLower(u.Scheme) != "https" || u.Host == "" {
		return fmt.Errorf("oauth: production issuer must use https")
	}
	return nil
}

// LoadSigningKey loads a persistent signing key when the PEM file exists.
func LoadSigningKey(paths SigningKeyPaths) (*KeySet, error) {
	if strings.TrimSpace(paths.SigningKey) == "" {
		return nil, nil
	}
	if _, err := os.Stat(paths.SigningKey); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return LoadKeySetFromPEM(paths.SigningKey, paths.SigningKID)
}
