package oauth

import "errors"

var (
	// ErrInvalidRequest is returned for malformed OAuth requests.
	ErrInvalidRequest = errors.New("oauth: invalid request")

	// ErrInvalidClient is returned when the client is unknown or misconfigured.
	ErrInvalidClient = errors.New("oauth: invalid client")

	// ErrUnauthorizedClient is returned when client authentication fails.
	ErrUnauthorizedClient = errors.New("oauth: unauthorized client")

	// ErrInvalidGrant is returned when an authorization code or refresh token is invalid.
	ErrInvalidGrant = errors.New("oauth: invalid grant")

	// ErrUnsupportedGrant is returned for unknown grant_type values.
	ErrUnsupportedGrant = errors.New("oauth: unsupported grant type")

	// ErrInvalidScope is returned when requested scopes exceed the client allow-list.
	ErrInvalidScope = errors.New("oauth: invalid scope")

	// ErrInvalidConfig is returned when server configuration is incomplete.
	ErrInvalidConfig = errors.New("oauth: invalid config")

	// ErrClientExists is returned when registering a client id that is already taken.
	ErrClientExists = errors.New("oauth: client already exists")

	// ErrAccessDenied is returned when the resource owner denies authorization.
	ErrAccessDenied = errors.New("oauth: access denied")
)
