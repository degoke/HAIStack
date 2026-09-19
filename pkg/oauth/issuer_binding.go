package oauth

import "strings"

// NormalizeIssuerURL trims and removes a trailing slash from an issuer URL.
func NormalizeIssuerURL(issuer string) string {
	return strings.TrimRight(strings.TrimSpace(issuer), "/")
}

// EntryIssuerMatches reports whether a stored row may be used by a server configured
// with serverIssuer. Legacy rows with an empty stored issuer remain valid for any
// server (single-issuer deployments before issuer binding).
func EntryIssuerMatches(entryIssuer, serverIssuer string) bool {
	entryIssuer = NormalizeIssuerURL(entryIssuer)
	serverIssuer = NormalizeIssuerURL(serverIssuer)
	if entryIssuer == "" {
		return true
	}
	return entryIssuer == serverIssuer
}
