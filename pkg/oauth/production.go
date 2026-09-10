package oauth

import (
	"fmt"
	"strings"
)

// ApplyProductionDefaults applies safer OAuth defaults for production-like hosts.
// Call after ApplySQLiteStores and before NewServer.
//
// When dynamic client registration remains enabled, RegistrationAccessToken must be set.
// Redirect URI validation always requires https except for loopback http hosts.
func ApplyProductionDefaults(cfg *Config) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	dcrEnabled := cfg.DynamicClientRegistration == nil || *cfg.DynamicClientRegistration
	if dcrEnabled && strings.TrimSpace(cfg.RegistrationAccessToken) == "" {
		return fmt.Errorf("%w: set RegistrationAccessToken or disable dynamic client registration for production", ErrInvalidConfig)
	}
	return nil
}
