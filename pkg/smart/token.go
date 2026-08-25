package smart

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TokenClaims holds normalized JWT/assertion claims relevant to SMART decisions.
type TokenClaims struct {
	Issuer    string    `json:"iss,omitempty"`
	Subject   string    `json:"sub,omitempty"`
	Audience  []string  `json:"aud,omitempty"`
	ExpiresAt time.Time `json:"exp,omitempty"`
	NotBefore time.Time `json:"nbf,omitempty"`
	IssuedAt  time.Time `json:"iat,omitempty"`
	JWTID     string    `json:"jti,omitempty"`
	ClientID  string    `json:"client_id,omitempty"`

	// Scope is the raw space-delimited scope claim.
	Scope  string   `json:"scope,omitempty"`
	Scopes ScopeSet `json:"-"`

	// Launch / FHIR context claims commonly present on SMART access tokens.
	Patient   string `json:"patient,omitempty"`
	Encounter string `json:"encounter,omitempty"`
	FHIRUser  string `json:"fhirUser,omitempty"`

	// TenantHint is an optional application-specific tenant claim.
	TenantHint string `json:"tenant,omitempty"`

	// LaunchExtensions holds additional string launch metadata from the token.
	LaunchExtensions map[string]string `json:"-"`

	// Extra retains unrecognized claims for host adapters.
	Extra map[string]any `json:"-"`
}

// TokenValidateOptions configures TokenValidator checks.
type TokenValidateOptions struct {
	// ExpectedIssuer, when non-empty, must equal claims.Issuer.
	ExpectedIssuer string
	// ExpectedAudience, when non-empty, must appear in claims.Audience.
	ExpectedAudience string
	// ExpectedAudiences, when non-empty, requires at least one match.
	ExpectedAudiences []string
	// RequiredScopes, when non-empty, must all be present (exact raw match).
	RequiredScopes []string
	// RequireScopesClaim fails when the scope claim is empty.
	RequireScopesClaim bool
	// RequireIssuer, RequireAudience, RequireExpiry, RequireSubject, and
	// RequireJWTID make the corresponding claims mandatory for assertion
	// validation. They are opt-in because gateway-validated access tokens may
	// intentionally omit some claims from a local trust boundary.
	RequireIssuer   bool
	RequireAudience bool
	RequireExpiry   bool
	RequireSubject  bool
	RequireJWTID    bool
	// Clock skew allowed for exp/nbf checks.
	ClockSkew time.Duration
	// Now overrides time.Now for tests.
	Now func() time.Time
	// SkipTimeChecks disables exp/nbf validation.
	SkipTimeChecks bool
}

// TokenValidator parses JWT structure and validates SMART-relevant claims.
// Signature verification is optional via SignatureVerifier so hosts can plug in
// crypto without making HTTP or browser runtimes a package dependency.
type TokenValidator struct {
	Verifier SignatureVerifier
	Now      func() time.Time
}

// NewTokenValidator returns a TokenValidator. verifier may be nil when only
// claim-shape validation is required (e.g. after an upstream gateway verified
// the signature).
func NewTokenValidator(verifier SignatureVerifier) *TokenValidator {
	return &TokenValidator{Verifier: verifier}
}

// ValidateToken parses a compact JWT, optionally verifies the signature, and
// validates iss/aud/exp/nbf/scope per opts.
func (v *TokenValidator) ValidateToken(token string, opts TokenValidateOptions) (TokenClaims, error) {
	parts, err := splitJWT(token)
	if err != nil {
		return TokenClaims{}, err
	}
	if v != nil && v.Verifier != nil {
		if err := v.Verifier.Verify(parts.HeaderSegment, parts.PayloadSegment, parts.Signature, parts.Alg); err != nil {
			return TokenClaims{}, err
		}
	}
	claims, err := claimsFromPayload(parts.Payload)
	if err != nil {
		return TokenClaims{}, err
	}
	if err := ValidateClaims(claims, opts.withNow(v)); err != nil {
		return TokenClaims{}, err
	}
	return claims, nil
}

// ParseTokenUnverified parses a compact JWT into TokenClaims without signature
// or time validation. Hosts that already trust the token may use this then call
// ValidateClaims.
func ParseTokenUnverified(token string) (TokenClaims, error) {
	parts, err := splitJWT(token)
	if err != nil {
		return TokenClaims{}, err
	}
	return claimsFromPayload(parts.Payload)
}

// ValidateClaims checks issuer, audience, time, and scope requirements.
func ValidateClaims(claims TokenClaims, opts TokenValidateOptions) error {
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	skew := opts.ClockSkew
	if skew < 0 {
		skew = 0
	}

	if opts.ExpectedIssuer != "" && claims.Issuer != opts.ExpectedIssuer {
		return fmt.Errorf("%w: got %q want %q", ErrIssuerMismatch, claims.Issuer, opts.ExpectedIssuer)
	}
	if opts.RequireIssuer && strings.TrimSpace(claims.Issuer) == "" {
		return fmt.Errorf("%w: issuer claim required", ErrInvalidToken)
	}

	audiences := opts.ExpectedAudiences
	if opts.ExpectedAudience != "" {
		audiences = append(append([]string(nil), audiences...), opts.ExpectedAudience)
	}
	if opts.RequireAudience && len(claims.Audience) == 0 {
		return fmt.Errorf("%w: audience claim required", ErrInvalidToken)
	}
	if len(audiences) > 0 && !audienceMatches(claims.Audience, audiences) {
		return fmt.Errorf("%w: token aud %v does not match %v", ErrAudienceMismatch, claims.Audience, audiences)
	}

	if !opts.SkipTimeChecks {
		if opts.RequireExpiry && claims.ExpiresAt.IsZero() {
			return fmt.Errorf("%w: exp claim required", ErrInvalidToken)
		}
		if !claims.ExpiresAt.IsZero() && now.After(claims.ExpiresAt.Add(skew)) {
			return fmt.Errorf("%w: exp %s", ErrTokenExpired, claims.ExpiresAt.UTC().Format(time.RFC3339))
		}
		if !claims.NotBefore.IsZero() && now.Add(skew).Before(claims.NotBefore) {
			return fmt.Errorf("%w: nbf %s", ErrTokenNotYetValid, claims.NotBefore.UTC().Format(time.RFC3339))
		}
	}
	if opts.RequireSubject && strings.TrimSpace(claims.Subject) == "" {
		return fmt.Errorf("%w: sub claim required", ErrInvalidToken)
	}
	if opts.RequireJWTID && strings.TrimSpace(claims.JWTID) == "" {
		return fmt.Errorf("%w: jti claim required", ErrInvalidToken)
	}

	if opts.RequireScopesClaim && strings.TrimSpace(claims.Scope) == "" && claims.Scopes.Empty() {
		return fmt.Errorf("%w: scope claim required", ErrMissingScopes)
	}
	if len(opts.RequiredScopes) > 0 {
		set := claims.Scopes
		if set.Empty() && claims.Scope != "" {
			parsed, err := ParseScopes(claims.Scope)
			if err != nil {
				return err
			}
			set = parsed
		}
		if !set.ContainsAll(opts.RequiredScopes) {
			return fmt.Errorf("%w: required %v in %v", ErrMissingScopes, opts.RequiredScopes, set.Strings())
		}
	}
	return nil
}

func (opts TokenValidateOptions) withNow(v *TokenValidator) TokenValidateOptions {
	if opts.Now != nil {
		return opts
	}
	if v != nil && v.Now != nil {
		opts.Now = v.Now
		return opts
	}
	return opts
}

type jwtParts struct {
	HeaderSegment  string
	PayloadSegment string
	Signature      []byte
	Alg            string
	KeyID          string
	Payload        []byte
}

func splitJWT(token string) (jwtParts, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return jwtParts{}, fmt.Errorf("%w: expected compact JWT with three segments", ErrInvalidToken)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtParts{}, fmt.Errorf("%w: header: %v", ErrInvalidToken, err)
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return jwtParts{}, fmt.Errorf("%w: header json: %v", ErrInvalidToken, err)
	}
	if header.Alg == "" {
		return jwtParts{}, fmt.Errorf("%w: missing alg", ErrInvalidToken)
	}
	if strings.EqualFold(header.Alg, "none") {
		return jwtParts{}, fmt.Errorf("%w: alg none is not allowed", ErrInvalidToken)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtParts{}, fmt.Errorf("%w: payload: %v", ErrInvalidToken, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtParts{}, fmt.Errorf("%w: signature: %v", ErrInvalidToken, err)
	}
	return jwtParts{
		HeaderSegment:  parts[0],
		PayloadSegment: parts[1],
		Signature:      sig,
		Alg:            header.Alg,
		KeyID:          header.Kid,
		Payload:        payload,
	}, nil
}

func claimsFromPayload(payload []byte) (TokenClaims, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return TokenClaims{}, fmt.Errorf("%w: payload json: %v", ErrInvalidToken, err)
	}
	if raw == nil {
		return TokenClaims{}, fmt.Errorf("%w: payload must be a JSON object", ErrInvalidToken)
	}
	claims := TokenClaims{
		Extra:            make(map[string]any),
		LaunchExtensions: make(map[string]string),
	}
	known := map[string]struct{}{
		"iss": {}, "sub": {}, "aud": {}, "exp": {}, "nbf": {}, "iat": {}, "jti": {},
		"client_id": {}, "scope": {}, "patient": {}, "encounter": {}, "fhirUser": {}, "tenant": {},
	}
	var err error
	if claims.Issuer, err = strictClaimString(raw, "iss"); err != nil {
		return TokenClaims{}, err
	}
	if claims.Subject, err = strictClaimString(raw, "sub"); err != nil {
		return TokenClaims{}, err
	}
	if claims.Audience, err = strictClaimStringSlice(raw, "aud"); err != nil {
		return TokenClaims{}, err
	}
	if claims.ExpiresAt, err = strictClaimTime(raw, "exp"); err != nil {
		return TokenClaims{}, err
	}
	if claims.NotBefore, err = strictClaimTime(raw, "nbf"); err != nil {
		return TokenClaims{}, err
	}
	if claims.IssuedAt, err = strictClaimTime(raw, "iat"); err != nil {
		return TokenClaims{}, err
	}
	if claims.JWTID, err = strictClaimString(raw, "jti"); err != nil {
		return TokenClaims{}, err
	}
	if claims.ClientID, err = strictClaimString(raw, "client_id"); err != nil {
		return TokenClaims{}, err
	}
	if claims.ClientID == "" {
		// Backend-service assertions often use iss/sub as the client id.
		claims.ClientID = claims.Issuer
	}
	if claims.Scope, err = strictClaimString(raw, "scope"); err != nil {
		return TokenClaims{}, err
	}
	if claims.Patient, err = strictClaimString(raw, "patient"); err != nil {
		return TokenClaims{}, err
	}
	if claims.Encounter, err = strictClaimString(raw, "encounter"); err != nil {
		return TokenClaims{}, err
	}
	if claims.FHIRUser, err = strictClaimString(raw, "fhirUser"); err != nil {
		return TokenClaims{}, err
	}
	if claims.TenantHint, err = strictClaimString(raw, "tenant"); err != nil {
		return TokenClaims{}, err
	}

	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		claims.Extra[k] = v
		if s := claimString(v); s != "" && safeLaunchExtension(k, s) {
			claims.LaunchExtensions[k] = s
		}
	}
	if len(claims.Extra) == 0 {
		claims.Extra = nil
	}
	if len(claims.LaunchExtensions) == 0 {
		claims.LaunchExtensions = nil
	}
	if claims.Scope != "" {
		scopes, err := ParseScopes(claims.Scope)
		if err != nil {
			return TokenClaims{}, err
		}
		claims.Scopes = scopes
	}
	return claims, nil
}

func safeLaunchExtension(key, value string) bool {
	if !strings.HasPrefix(key, "launch") || len(key) > 128 || len(value) > 4096 {
		return false
	}
	for i, r := range key {
		if i == 0 {
			continue
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func strictClaimString(raw map[string]any, name string) (string, error) {
	v, ok := raw[name]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%w: claim %s must be a string", ErrInvalidToken, name)
	}
	return s, nil
}

func strictClaimStringSlice(raw map[string]any, name string) ([]string, error) {
	v, ok := raw[name]
	if !ok || v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: claim %s must be a string or string array", ErrInvalidToken, name)
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("%w: claim %s must contain only strings", ErrInvalidToken, name)
		}
		out[i] = s
	}
	return out, nil
}

func strictClaimTime(raw map[string]any, name string) (time.Time, error) {
	v, ok := raw[name]
	if !ok || v == nil {
		return time.Time{}, nil
	}
	n, ok := v.(float64)
	if !ok || n != float64(int64(n)) {
		return time.Time{}, fmt.Errorf("%w: claim %s must be an integer NumericDate", ErrInvalidToken, name)
	}
	return time.Unix(int64(n), 0).UTC(), nil
}

func claimString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func audienceMatches(tokenAud, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(tokenAud))
	for _, a := range tokenAud {
		set[a] = struct{}{}
	}
	for _, want := range expected {
		if _, ok := set[want]; ok {
			return true
		}
	}
	return false
}

// ExtractLaunchContext builds LaunchContext from token claims and optional overlays.
func ExtractLaunchContext(claims TokenClaims, overlay LaunchContextInput) LaunchContext {
	overlay.Claims = &claims
	return BuildLaunchContext(overlay)
}
