package smart

import (
	"fmt"
	"strings"
	"time"
)

// BackendClient describes a registered SMART backend-service client.
type BackendClient struct {
	ClientID      string            `json:"clientId"`
	AllowedScopes []string          `json:"allowedScopes,omitempty"`
	Key           ClientKeyMetadata `json:"key"`
	// Issuer defaults to ClientID when empty (common for client assertions).
	Issuer string `json:"issuer,omitempty"`
	// TenantHint optionally seeds auth tenant context for this client.
	TenantHint string `json:"tenantHint,omitempty"`
	// DisplayName is optional metadata for the derived service principal.
	DisplayName string `json:"displayName,omitempty"`
}

// EffectiveIssuer returns Issuer or ClientID.
func (c BackendClient) EffectiveIssuer() string {
	if strings.TrimSpace(c.Issuer) != "" {
		return strings.TrimSpace(c.Issuer)
	}
	return strings.TrimSpace(c.ClientID)
}

// BackendServiceAuth validates backend-service client assertions and maps them
// toward service principals. It does not implement an OAuth authorization server
// or token endpoint; hosts provide transport and token exchange.
type BackendServiceAuth struct {
	Clients map[string]BackendClient
	// Audience is the expected assertion audience (typically the token endpoint URL).
	Audience string
	Now      func() time.Time
	// ClockSkew allowed for exp/nbf.
	ClockSkew time.Duration
	// RequireScopeClaim requires a non-empty scope claim on the assertion.
	RequireScopeClaim bool
}

// NewBackendServiceAuth returns a BackendServiceAuth indexed by client id.
func NewBackendServiceAuth(audience string, clients ...BackendClient) (*BackendServiceAuth, error) {
	b := &BackendServiceAuth{
		Clients:  make(map[string]BackendClient, len(clients)),
		Audience: audience,
	}
	for _, c := range clients {
		if err := b.RegisterClient(c); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// RegisterClient upserts a backend client.
func (b *BackendServiceAuth) RegisterClient(c BackendClient) error {
	if b == nil {
		return fmt.Errorf("%w: backend service auth is nil", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("%w: client id required", ErrInvalidConfig)
	}
	if b.Clients == nil {
		b.Clients = make(map[string]BackendClient)
	}
	b.Clients[c.ClientID] = c
	return nil
}

// LookupClient returns a registered client.
func (b *BackendServiceAuth) LookupClient(clientID string) (BackendClient, error) {
	if b == nil || b.Clients == nil {
		return BackendClient{}, fmt.Errorf("%w: %s", ErrClientNotAllowed, clientID)
	}
	c, ok := b.Clients[clientID]
	if !ok {
		return BackendClient{}, fmt.Errorf("%w: %s", ErrClientNotAllowed, clientID)
	}
	return c, nil
}

// ValidateBackendAssertion parses and validates a signed client assertion JWT
// for a known backend client. Scopes on the assertion must be a subset of the
// client's AllowedScopes when that allow-list is non-empty.
func (b *BackendServiceAuth) ValidateBackendAssertion(assertion string, opts TokenValidateOptions) (TokenClaims, BackendClient, error) {
	if b == nil {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: backend service auth is nil", ErrInvalidConfig)
	}
	// Peek issuer/client without verifying so we can select the client key.
	unverified, err := ParseTokenUnverified(assertion)
	if err != nil {
		return TokenClaims{}, BackendClient{}, err
	}
	clientID := firstNonEmpty(unverified.ClientID, unverified.Issuer, unverified.Subject)
	client, err := b.LookupClient(clientID)
	if err != nil {
		return TokenClaims{}, BackendClient{}, err
	}

	expectedIssuer := client.EffectiveIssuer()
	audiences := opts.ExpectedAudiences
	if opts.ExpectedAudience != "" {
		audiences = append(audiences, opts.ExpectedAudience)
	}
	if len(audiences) == 0 && b.Audience != "" {
		opts.ExpectedAudience = b.Audience
	}
	if opts.ExpectedIssuer == "" {
		opts.ExpectedIssuer = expectedIssuer
	}
	if opts.Now == nil {
		opts.Now = b.Now
	}
	if opts.ClockSkew == 0 {
		opts.ClockSkew = b.ClockSkew
	}
	if b.RequireScopeClaim {
		opts.RequireScopesClaim = true
	}

	var verifier SignatureVerifier
	if strings.TrimSpace(client.Key.PublicKeyPEM) != "" {
		verifier = PEMVerifier{
			PublicKeyPEM: client.Key.PublicKeyPEM,
			Algorithm:    client.Key.Algorithm,
		}
	}
	tv := NewTokenValidator(verifier)
	tv.Now = b.Now

	claims, err := tv.ValidateToken(assertion, opts)
	if err != nil {
		return TokenClaims{}, BackendClient{}, err
	}

	// SMART backend assertions typically set iss == sub == client_id.
	if claims.Subject != "" && claims.Subject != client.ClientID && claims.Subject != expectedIssuer {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: subject %q does not match client %q", ErrInvalidToken, claims.Subject, client.ClientID)
	}

	if err := b.requireAllowedScopes(client, claims.Scopes); err != nil {
		return TokenClaims{}, BackendClient{}, err
	}
	if claims.TenantHint == "" {
		claims.TenantHint = client.TenantHint
	}
	return claims, client, nil
}

func (b *BackendServiceAuth) requireAllowedScopes(client BackendClient, granted ScopeSet) error {
	if len(client.AllowedScopes) == 0 {
		return nil
	}
	allowed, err := ParseScopes(strings.Join(client.AllowedScopes, " "))
	if err != nil {
		return fmt.Errorf("%w: client allow-list: %v", ErrInvalidConfig, err)
	}
	if granted.Empty() {
		if b != nil && b.RequireScopeClaim {
			return fmt.Errorf("%w: assertion scopes required", ErrMissingScopes)
		}
		return nil
	}
	if !granted.SubsetOf(allowed) {
		return fmt.Errorf("%w: granted %v not in allow-list %v", ErrScopeNotAllowed, granted.Strings(), allowed.Strings())
	}
	return nil
}

// AuthorizeBackendScopes checks whether requested scopes are permitted for the client
// without validating a token (useful before token exchange).
func (b *BackendServiceAuth) AuthorizeBackendScopes(clientID string, requested ScopeSet) error {
	client, err := b.LookupClient(clientID)
	if err != nil {
		return err
	}
	return b.requireAllowedScopes(client, requested)
}
