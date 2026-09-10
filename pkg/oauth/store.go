package oauth

import "time"

// AuthorizationCodeStore persists single-use authorization codes.
type AuthorizationCodeStore interface {
	Issue(entry AuthCode) (string, error)
	Exchange(code, clientID, redirectURI, codeVerifier string) (*AuthCode, error)
}

// RefreshRecord holds refresh-token session material.
type RefreshRecord struct {
	Token      string
	ClientID   string
	Scope      string
	Subject    string
	Patient    string
	Encounter  string
	FHIRUser   string
	TenantHint string
	ExpiresAt  time.Time
	Revoked    bool
}

// RefreshTokenStore persists refresh tokens with optional rotation.
type RefreshTokenStore interface {
	Issue(record RefreshRecord) (string, error)
	Rotate(token, clientID string) (*RefreshRecord, string, error)
	Revoke(token string) error
	Lookup(token string) (*RefreshRecord, error)
}

// TokenRevocationStore tracks revoked token identifiers until expiry.
type TokenRevocationStore interface {
	Revoke(tokenID, tokenType string, expiresAt time.Time) error
	IsRevoked(tokenID string) (bool, error)
}
