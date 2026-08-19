package smart

import "errors"

var (
	// ErrInvalidScope is returned when a scope string cannot be parsed.
	ErrInvalidScope = errors.New("smart: invalid scope")

	// ErrInvalidToken is returned when a token or assertion is malformed.
	ErrInvalidToken = errors.New("smart: invalid token")

	// ErrTokenExpired is returned when exp is in the past.
	ErrTokenExpired = errors.New("smart: token expired")

	// ErrTokenNotYetValid is returned when nbf is in the future.
	ErrTokenNotYetValid = errors.New("smart: token not yet valid")

	// ErrIssuerMismatch is returned when iss does not match the expected issuer.
	ErrIssuerMismatch = errors.New("smart: issuer mismatch")

	// ErrAudienceMismatch is returned when aud does not include the expected audience.
	ErrAudienceMismatch = errors.New("smart: audience mismatch")

	// ErrMissingScopes is returned when required scopes are absent.
	ErrMissingScopes = errors.New("smart: missing scopes")

	// ErrClientNotAllowed is returned when a backend client is unknown or denied.
	ErrClientNotAllowed = errors.New("smart: client not allowed")

	// ErrScopeNotAllowed is returned when requested scopes exceed a client's allow-list.
	ErrScopeNotAllowed = errors.New("smart: scope not allowed")

	// ErrSignatureInvalid is returned when JWT signature verification fails.
	ErrSignatureInvalid = errors.New("smart: signature invalid")

	// ErrInvalidConfig is returned when adapter or validator config is incomplete.
	ErrInvalidConfig = errors.New("smart: invalid config")

	// ErrMissingKey is returned when signature verification requires a key that is absent.
	ErrMissingKey = errors.New("smart: missing key")

	// ErrReplay is returned when a JWT assertion identifier has already been used.
	ErrReplay = errors.New("smart: replayed assertion")
)
