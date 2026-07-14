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

	audiences := opts.ExpectedAudiences
	if opts.ExpectedAudience != "" {
		audiences = append(append([]string(nil), audiences...), opts.ExpectedAudience)
	}
	if len(audiences) > 0 && !audienceMatches(claims.Audience, audiences) {
		return fmt.Errorf("%w: token aud %v does not match %v", ErrAudienceMismatch, claims.Audience, audiences)
	}

	if !opts.SkipTimeChecks {
		if !claims.ExpiresAt.IsZero() && now.After(claims.ExpiresAt.Add(skew)) {
			return fmt.Errorf("%w: exp %s", ErrTokenExpired, claims.ExpiresAt.UTC().Format(time.RFC3339))
		}
		if !claims.NotBefore.IsZero() && now.Add(skew).Before(claims.NotBefore) {
			return fmt.Errorf("%w: nbf %s", ErrTokenNotYetValid, claims.NotBefore.UTC().Format(time.RFC3339))
		}
	}

	if opts.RequireScopesClaim && claims.Scope == "" && claims.Scopes.Empty() {
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
	Payload        []byte
}

func splitJWT(token string) (jwtParts, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return jwtParts{}, fmt.Errorf("%w: expected compact JWT with three segments", ErrInvalidToken)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtParts{}, fmt.Errorf("%w: header: %v", ErrInvalidToken, err)
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return jwtParts{}, fmt.Errorf("%w: header json: %v", ErrInvalidToken, err)
	}
	if header.Alg == "" {
		return jwtParts{}, fmt.Errorf("%w: missing alg", ErrInvalidToken)
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
		Payload:        payload,
	}, nil
}

func claimsFromPayload(payload []byte) (TokenClaims, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return TokenClaims{}, fmt.Errorf("%w: payload json: %v", ErrInvalidToken, err)
	}
	claims := TokenClaims{
		Extra:            make(map[string]any),
		LaunchExtensions: make(map[string]string),
	}
	known := map[string]struct{}{
		"iss": {}, "sub": {}, "aud": {}, "exp": {}, "nbf": {}, "iat": {}, "jti": {},
		"client_id": {}, "scope": {}, "patient": {}, "encounter": {}, "fhirUser": {}, "tenant": {},
	}
	claims.Issuer = claimString(raw["iss"])
	claims.Subject = claimString(raw["sub"])
	claims.Audience = claimStringSlice(raw["aud"])
	claims.ExpiresAt = claimTime(raw["exp"])
	claims.NotBefore = claimTime(raw["nbf"])
	claims.IssuedAt = claimTime(raw["iat"])
	claims.JWTID = claimString(raw["jti"])
	claims.ClientID = claimString(raw["client_id"])
	if claims.ClientID == "" {
		// Backend-service assertions often use iss/sub as the client id.
		claims.ClientID = claims.Issuer
	}
	claims.Scope = claimString(raw["scope"])
	claims.Patient = claimString(raw["patient"])
	claims.Encounter = claimString(raw["encounter"])
	claims.FHIRUser = claimString(raw["fhirUser"])
	claims.TenantHint = claimString(raw["tenant"])

	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		claims.Extra[k] = v
		if s := claimString(v); s != "" && strings.HasPrefix(k, "launch") {
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

func claimStringSlice(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := claimString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	default:
		return nil
	}
}

func claimTime(v any) time.Time {
	switch t := v.(type) {
	case float64:
		return time.Unix(int64(t), 0).UTC()
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return time.Time{}
		}
		return time.Unix(n, 0).UTC()
	case int64:
		return time.Unix(t, 0).UTC()
	case int:
		return time.Unix(int64(t), 0).UTC()
	default:
		return time.Time{}
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
