package oauth

import (
	"fmt"
	"strings"
)

// NormalizeIssuerURL trims and removes a trailing slash from an issuer URL.
func NormalizeIssuerURL(issuer string) string {
	return strings.TrimRight(strings.TrimSpace(issuer), "/")
}

// EntryIssuerMatches reports whether a stored row belongs to serverIssuer.
// Both issuers must be non-empty and equal after normalization.
func EntryIssuerMatches(entryIssuer, serverIssuer string) bool {
	entryIssuer = NormalizeIssuerURL(entryIssuer)
	serverIssuer = NormalizeIssuerURL(serverIssuer)
	return entryIssuer != "" && entryIssuer == serverIssuer
}

// RequireBoundIssuer returns a normalized issuer or an error when missing.
func RequireBoundIssuer(issuer string) (string, error) {
	iss := NormalizeIssuerURL(issuer)
	if iss == "" {
		return "", fmt.Errorf("oauth: issuer required")
	}
	return iss, nil
}
