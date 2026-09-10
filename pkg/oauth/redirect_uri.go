package oauth

import (
	"fmt"
	"net/url"
	"strings"
)

func validateRedirectURIs(redirectURIs []string) error {
	for _, raw := range redirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateRedirectURI(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: redirect_uri must not be empty", ErrInvalidConfig)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: invalid redirect_uri %q", ErrInvalidConfig, raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: redirect_uri must use https except for loopback hosts", ErrInvalidConfig)
	default:
		return fmt.Errorf("%w: redirect_uri scheme %q not allowed", ErrInvalidConfig, u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
