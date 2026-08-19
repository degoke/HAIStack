package smart

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ReplayStore atomically records a JWT ID until its expiry and rejects IDs
// already recorded. Hosts may provide a distributed implementation for
// multi-instance deployments.
type ReplayStore interface {
	CheckAndStore(jti string, expiresAt time.Time) error
}

// BackendClientStore is an optional persistent/dynamic registry boundary for
// backend clients. Implementations can load credentials and allow-list changes
// without rebuilding the process.
type BackendClientStore interface {
	LookupBackendClient(clientID string) (BackendClient, error)
	RegisterBackendClient(client BackendClient) error
}

// MemoryReplayStore is a process-local replay store suitable for development
// and single-instance deployments.
type MemoryReplayStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	// Now is injectable for deterministic tests and hosts with a shared clock.
	// A nil function uses time.Now.
	Now func() time.Time
}

func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{entries: make(map[string]time.Time)}
}

func (s *MemoryReplayStore) CheckAndStore(jti string, expiresAt time.Time) error {
	if s == nil || strings.TrimSpace(jti) == "" {
		return fmt.Errorf("%w: jti required", ErrReplay)
	}
	nowFn := s.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, expiry := range s.entries {
		if !expiry.IsZero() && now.After(expiry) {
			delete(s.entries, key)
		}
	}
	if s.entries == nil {
		s.entries = make(map[string]time.Time)
	}
	if _, exists := s.entries[jti]; exists {
		return fmt.Errorf("%w: jti %q", ErrReplay, jti)
	}
	s.entries[jti] = expiresAt
	return nil
}

// BackendClient describes a registered SMART backend-service client.
type BackendClient struct {
	ClientID      string   `json:"clientId"`
	AllowedScopes []string `json:"allowedScopes,omitempty"`
	// AllowAnyScope permits assertions with any scope when AllowedScopes is empty.
	// By default clients must declare an explicit allow-list.
	AllowAnyScope bool              `json:"allowAnyScope,omitempty"`
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
	Clients     map[string]BackendClient
	ClientStore BackendClientStore
	// Audience is the expected assertion audience (typically the token endpoint URL).
	Audience string
	Now      func() time.Time
	// ClockSkew allowed for exp/nbf.
	ClockSkew time.Duration
	// RequireScopeClaim requires a non-empty scope claim on the assertion.
	RequireScopeClaim bool
	// VerifierForClient may resolve a rotating/JWKS-backed verifier. When nil,
	// inline client PEM metadata is used.
	VerifierForClient func(BackendClient) SignatureVerifier
	// Replay records assertion jti values. Nil lazily gets a process-local store.
	Replay   ReplayStore
	mu       sync.RWMutex
	replayMu sync.Mutex
}

// NewBackendServiceAuth returns a BackendServiceAuth indexed by client id.
func NewBackendServiceAuth(audience string, clients ...BackendClient) (*BackendServiceAuth, error) {
	return NewBackendServiceAuthWithStores(audience, nil, nil, clients...)
}

// NewBackendServiceAuthWithStores constructs backend auth with explicit
// client-registry and replay persistence. Nil stores use process-local memory.
func NewBackendServiceAuthWithStores(audience string, clientStore BackendClientStore, replay ReplayStore, clients ...BackendClient) (*BackendServiceAuth, error) {
	if replay == nil {
		replay = NewMemoryReplayStore()
	}
	b := &BackendServiceAuth{
		Clients:     make(map[string]BackendClient, len(clients)),
		ClientStore: clientStore,
		Audience:    audience,
		Replay:      replay,
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
	if len(c.AllowedScopes) == 0 && !c.AllowAnyScope {
		return fmt.Errorf("%w: client %q must define allowedScopes or set allowAnyScope", ErrInvalidConfig, c.ClientID)
	}
	if len(c.AllowedScopes) > 0 {
		if _, err := ParseScopes(strings.Join(c.AllowedScopes, " ")); err != nil {
			return fmt.Errorf("%w: client %q allowedScopes: %v", ErrInvalidConfig, c.ClientID, err)
		}
	}
	if b.ClientStore != nil {
		if err := b.ClientStore.RegisterBackendClient(c); err != nil {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Clients == nil {
		b.Clients = make(map[string]BackendClient)
	}
	b.Clients[c.ClientID] = c
	return nil
}

// LookupClient returns a registered client.
func (b *BackendServiceAuth) LookupClient(clientID string) (BackendClient, error) {
	if b == nil {
		return BackendClient{}, fmt.Errorf("%w: %s", ErrClientNotAllowed, clientID)
	}
	if b.ClientStore != nil {
		client, err := b.ClientStore.LookupBackendClient(clientID)
		if err != nil {
			return BackendClient{}, fmt.Errorf("%w: %s", ErrClientNotAllowed, clientID)
		}
		return client, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
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
	parts, err := splitJWT(assertion)
	if err != nil {
		return TokenClaims{}, BackendClient{}, err
	}
	if client.Key.KeyID != "" && parts.KeyID != client.Key.KeyID {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: key id %q does not match configured key id %q", ErrSignatureInvalid, parts.KeyID, client.Key.KeyID)
	}

	expectedIssuer := client.EffectiveIssuer()
	localOpts := opts
	// The service-level audience is a trust-boundary requirement. Caller
	// options may add expected audiences, but must not replace the configured
	// audience with a weaker alternative.
	if b.Audience != "" {
		localOpts.ExpectedAudience = b.Audience
	}
	if b.Audience == "" && len(localOpts.ExpectedAudiences) == 0 && localOpts.ExpectedAudience == "" {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: expected audience is required", ErrInvalidConfig)
	}
	if localOpts.ExpectedIssuer == "" {
		localOpts.ExpectedIssuer = expectedIssuer
	}
	if localOpts.Now == nil {
		localOpts.Now = b.Now
	}
	if localOpts.ClockSkew == 0 {
		localOpts.ClockSkew = b.ClockSkew
	}
	if b.RequireScopeClaim {
		localOpts.RequireScopesClaim = true
	}
	localOpts.RequireIssuer = true
	localOpts.RequireAudience = true
	localOpts.RequireExpiry = true
	localOpts.RequireSubject = true
	localOpts.RequireJWTID = true
	// Backend assertions are always checked against the current clock. A
	// caller cannot weaken this trust boundary by passing SkipTimeChecks.
	localOpts.SkipTimeChecks = false

	var verifier SignatureVerifier
	if b.VerifierForClient != nil {
		verifier = b.VerifierForClient(client)
	} else if strings.TrimSpace(client.Key.PublicKeyPEM) != "" {
		verifier = PEMVerifier{
			PublicKeyPEM: client.Key.PublicKeyPEM,
			Algorithm:    client.Key.Algorithm,
		}
	}
	if verifier == nil {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: client %q requires a verification key", ErrMissingKey, client.ClientID)
	}
	tv := NewTokenValidator(verifier)
	tv.Now = b.Now

	claims, err := tv.ValidateToken(assertion, localOpts)
	if err != nil {
		return TokenClaims{}, BackendClient{}, err
	}

	// SMART backend assertions typically set iss == sub == client_id.
	if claims.Subject != client.ClientID {
		return TokenClaims{}, BackendClient{}, fmt.Errorf("%w: subject %q does not match client %q", ErrInvalidToken, claims.Subject, client.ClientID)
	}

	if err := b.requireAllowedScopes(client, claims.Scopes); err != nil {
		return TokenClaims{}, BackendClient{}, err
	}
	if claims.JWTID != "" {
		if replay := b.replayStore(); replay != nil {
			if err := replay.CheckAndStore(claims.JWTID, claims.ExpiresAt); err != nil {
				return TokenClaims{}, BackendClient{}, err
			}
		}
	}
	if claims.TenantHint == "" {
		claims.TenantHint = client.TenantHint
	}
	return claims, client, nil
}

func (b *BackendServiceAuth) replayStore() ReplayStore {
	if b == nil {
		return nil
	}
	b.replayMu.Lock()
	defer b.replayMu.Unlock()
	if b.Replay == nil {
		b.Replay = &MemoryReplayStore{Now: b.Now, entries: make(map[string]time.Time)}
	} else if memory, ok := b.Replay.(*MemoryReplayStore); ok && memory.Now == nil && b.Now != nil {
		memory.Now = b.Now
	}
	return b.Replay
}

func (b *BackendServiceAuth) requireAllowedScopes(client BackendClient, granted ScopeSet) error {
	if len(client.AllowedScopes) == 0 {
		if client.AllowAnyScope {
			return nil
		}
		return fmt.Errorf("%w: client %q has no allowedScopes configured", ErrInvalidConfig, client.ClientID)
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
