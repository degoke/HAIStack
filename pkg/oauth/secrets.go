package oauth

import "crypto/subtle"

// secretEqual compares two secret strings in constant time.
func secretEqual(got, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// tokenEqual compares opaque tokens (for example CSRF values) in constant time.
func tokenEqual(got, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// TokenEqual compares opaque tokens in constant time.
func TokenEqual(got, expected string) bool {
	return tokenEqual(got, expected)
}
