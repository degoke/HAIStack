package oauth

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

const defaultAccessTokenTTL = time.Hour

// TokenSigner signs JWT access tokens issued by the authorization server.
type TokenSigner interface {
	Algorithm() string
	KeyID() string
	Sign(headerSegment, payloadSegment string) ([]byte, error)
	Verifier() smart.SignatureVerifier
}

// RS256Signer signs access tokens with an RSA private key.
type RS256Signer struct {
	PrivateKey *rsa.PrivateKey
	Kid        string
}

func (s RS256Signer) Algorithm() string { return "RS256" }
func (s RS256Signer) KeyID() string     { return s.Kid }

func (s RS256Signer) Sign(headerSegment, payloadSegment string) ([]byte, error) {
	if s.PrivateKey == nil {
		return nil, fmt.Errorf("%w: rsa private key required", ErrInvalidConfig)
	}
	sum := sha256.Sum256([]byte(headerSegment + "." + payloadSegment))
	return rsa.SignPKCS1v15(rand.Reader, s.PrivateKey, crypto.SHA256, sum[:])
}

func (s RS256Signer) Verifier() smart.SignatureVerifier {
	if s.PrivateKey == nil {
		return nil
	}
	pubPEM, err := marshalPublicKeyPEM(&s.PrivateKey.PublicKey)
	if err != nil {
		return nil
	}
	return smart.PEMVerifier{PublicKeyPEM: pubPEM, Algorithm: "RS256"}
}

// HS256Signer signs access tokens with an HMAC secret.
type HS256Signer struct {
	Secret []byte
	Kid    string
}

func (s HS256Signer) Algorithm() string { return "HS256" }
func (s HS256Signer) KeyID() string     { return s.Kid }

func (s HS256Signer) Sign(headerSegment, payloadSegment string) ([]byte, error) {
	if len(s.Secret) == 0 {
		return nil, fmt.Errorf("%w: hmac secret required", ErrInvalidConfig)
	}
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write([]byte(headerSegment + "." + payloadSegment))
	return mac.Sum(nil), nil
}

func (s HS256Signer) Verifier() smart.SignatureVerifier {
	if len(s.Secret) == 0 {
		return nil
	}
	secret := append([]byte(nil), s.Secret...)
	return smart.SignatureVerifierFunc(func(headerSegment, payloadSegment string, signature []byte, alg string) error {
		if alg != "HS256" {
			return fmt.Errorf("%w: alg %q", smart.ErrSignatureInvalid, alg)
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(headerSegment + "." + payloadSegment))
		if !hmac.Equal(mac.Sum(nil), signature) {
			return smart.ErrSignatureInvalid
		}
		return nil
	})
}

// AccessTokenClaims describes claims placed on issued access tokens.
type AccessTokenClaims struct {
	Subject    string
	ClientID   string
	Scope      string
	Audience   string
	Patient    string
	Encounter  string
	FHIRUser   string
	TenantHint string
	TTL        time.Duration
	JTI        string
}

// IssueAccessToken builds and signs a JWT access token.
func IssueAccessToken(signer TokenSigner, issuer string, claims AccessTokenClaims) (string, time.Time, error) {
	payload, exp, err := buildTokenPayload(issuer, claims, true)
	if err != nil {
		return "", time.Time{}, err
	}
	token, err := signJWTPayload(signer, payload)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// IssueIDToken builds a signed OIDC id_token.
func IssueIDToken(signer TokenSigner, issuer string, clientID string, claims AccessTokenClaims) (string, error) {
	claims.Audience = firstNonEmpty(claims.Audience, clientID)
	payload, _, err := buildTokenPayload(issuer, claims, false)
	if err != nil {
		return "", err
	}
	delete(payload, "scope")
	delete(payload, "client_id")
	return signJWTPayload(signer, payload)
}

func buildTokenPayload(issuer string, claims AccessTokenClaims, includeAccessFields bool) (map[string]any, time.Time, error) {
	issuer = trimSlash(issuer)
	if issuer == "" {
		return nil, time.Time{}, fmt.Errorf("%w: issuer required", ErrInvalidConfig)
	}
	ttl := claims.TTL
	if ttl <= 0 {
		ttl = defaultAccessTokenTTL
	}
	now := time.Now().UTC()
	exp := now.Add(ttl)
	subject := firstNonEmpty(claims.Subject, claims.ClientID)
	if subject == "" {
		return nil, time.Time{}, fmt.Errorf("%w: subject or client id required", ErrInvalidConfig)
	}
	aud := strings.TrimSpace(claims.Audience)
	if aud == "" {
		return nil, time.Time{}, fmt.Errorf("%w: audience required", ErrInvalidConfig)
	}
	payload := map[string]any{
		"iss": issuer,
		"sub": subject,
		"aud": aud,
		"iat": now.Unix(),
		"exp": exp.Unix(),
	}
	if includeAccessFields {
		jti := strings.TrimSpace(claims.JTI)
		if jti == "" {
			jti = formatJTI(now)
		}
		payload["jti"] = jti
		if claims.ClientID != "" {
			payload["client_id"] = claims.ClientID
		}
		if claims.Scope != "" {
			payload["scope"] = claims.Scope
		}
	}
	if claims.Patient != "" {
		payload["patient"] = claims.Patient
	}
	if claims.Encounter != "" {
		payload["encounter"] = claims.Encounter
	}
	if claims.FHIRUser != "" {
		payload["fhirUser"] = claims.FHIRUser
	}
	if claims.TenantHint != "" {
		payload["tenant"] = claims.TenantHint
	}
	return payload, exp, nil
}

func signJWTPayload(signer TokenSigner, payload map[string]any) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("%w: token signer required", ErrInvalidConfig)
	}
	header := map[string]string{
		"alg": signer.Algorithm(),
		"typ": "JWT",
	}
	if kid := signer.KeyID(); kid != "" {
		header["kid"] = kid
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headerSeg := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadSeg := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig, err := signer.Sign(headerSeg, payloadSeg)
	if err != nil {
		return "", err
	}
	return headerSeg + "." + payloadSeg + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func formatJTI(now time.Time) string {
	return fmt.Sprintf("%d", now.UnixNano())
}

func marshalPublicKeyPEM(key *rsa.PublicKey) (string, error) {
	if key == nil {
		return "", fmt.Errorf("%w: public key required", ErrInvalidConfig)
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ParseAccessTokenJTI returns the jti claim from an access token without verification.
func ParseAccessTokenJTI(token string) (string, time.Time, error) {
	claims, err := smart.ParseTokenUnverified(token)
	if err != nil {
		return "", time.Time{}, err
	}
	return claims.JWTID, claims.ExpiresAt, nil
}
