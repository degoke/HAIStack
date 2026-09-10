package oauth

import (
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// DefaultRegisteredClientScopes is the SMART scope allow-list applied to dynamically
// registered clients that omit scope, and the maximum scopes DCR may request.
func DefaultRegisteredClientScopes() []string {
	return []string{
		"openid",
		"offline_access",
		"patient/*.read",
		"user/*.read",
		"launch/patient",
	}
}

func registeredClientScopeAllowList(cfg Config) []string {
	if len(cfg.RegisteredClientScopes) > 0 {
		return append([]string(nil), cfg.RegisteredClientScopes...)
	}
	if len(cfg.ScopesSupported) > 0 {
		return append([]string(nil), cfg.ScopesSupported...)
	}
	return DefaultRegisteredClientScopes()
}

func NormalizeRegisteredClientScopes(cfg Config, requested []string) ([]string, error) {
	return normalizeRegisteredClientScopes(cfg, requested)
}

func normalizeRegisteredClientScopes(cfg Config, requested []string) ([]string, error) {
	allowed := registeredClientScopeAllowList(cfg)
	if len(requested) == 0 {
		return append([]string(nil), allowed...), nil
	}
	granted, err := smart.ParseScopes(strings.Join(requested, " "))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidScope, err)
	}
	allowedSet, err := smart.ParseScopes(strings.Join(allowed, " "))
	if err != nil {
		return nil, err
	}
	if !granted.SubsetOf(allowedSet) {
		return nil, fmt.Errorf("%w: requested scopes exceed server allow-list", ErrInvalidScope)
	}
	return granted.Strings(), nil
}
