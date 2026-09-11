package oauth

import (
	"fmt"
	"net/url"
	"strings"
)

// ApplyProductionDefaults applies safer OAuth defaults for production-like hosts.
// Call after ApplySQLiteStores and before NewServer.
//
// Production requires an https issuer, AutoApprove disabled, encrypted signing key
// storage secret, PKCE for all clients, and when dynamic client registration remains
// enabled, RegistrationAccessToken must be set.
func ApplyProductionDefaults(cfg *Config) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	if err := ValidateProductionIssuer(cfg.Issuer); err != nil {
		return err
	}
	if cfg.AutoApprove != nil && *cfg.AutoApprove {
		return fmt.Errorf("%w: AutoApprove must be false for production", ErrInvalidConfig)
	}
	if err := RequireSigningKeyEncryptionSecret(); err != nil {
		return err
	}
	requirePKCE := true
	cfg.RequirePKCEForAllClients = &requirePKCE
	dcrEnabled := cfg.DynamicClientRegistration == nil || *cfg.DynamicClientRegistration
	if dcrEnabled && strings.TrimSpace(cfg.RegistrationAccessToken) == "" {
		return fmt.Errorf("%w: set RegistrationAccessToken or disable dynamic client registration for production", ErrInvalidConfig)
	}
	return nil
}

// ValidateProductionIssuer requires a non-empty https issuer URL.
func ValidateProductionIssuer(issuer string) error {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return fmt.Errorf("%w: issuer URL is required for production", ErrInvalidConfig)
	}
	u, err := url.Parse(issuer)
	if err != nil || strings.ToLower(u.Scheme) != "https" || u.Host == "" {
		return fmt.Errorf("%w: production issuer must use https", ErrInvalidConfig)
	}
	return nil
}
