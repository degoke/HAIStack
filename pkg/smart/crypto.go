package smart

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash"
	"math/big"
	"strings"
)

// SignatureVerifier verifies a compact JWT signature over header.payload.
type SignatureVerifier interface {
	Verify(headerSegment, payloadSegment string, signature []byte, alg string) error
}

// SignatureVerifierFunc adapts a function to SignatureVerifier.
type SignatureVerifierFunc func(headerSegment, payloadSegment string, signature []byte, alg string) error

// Verify implements SignatureVerifier.
func (f SignatureVerifierFunc) Verify(headerSegment, payloadSegment string, signature []byte, alg string) error {
	return f(headerSegment, payloadSegment, signature, alg)
}

// ClientKeyMetadata describes key material used to validate backend-service
// client assertions. JWKS URI is recorded for host resolvers; v1 verifies from
// inline PEM when present.
type ClientKeyMetadata struct {
	// Algorithm is the expected JWS alg (RS256, RS384, ES256, ES384).
	Algorithm string `json:"algorithm,omitempty"`
	// PublicKeyPEM is a PEM-encoded PKIX public key or certificate.
	PublicKeyPEM string `json:"publicKeyPem,omitempty"`
	// JWKSURI is an optional discovery hint for host-provided key resolution.
	JWKSURI string `json:"jwksUri,omitempty"`
	// KeyID is an optional kid matching the assertion header.
	KeyID string `json:"keyId,omitempty"`
}

// PEMVerifier verifies JWT signatures with a PEM-encoded RSA or ECDSA public key.
type PEMVerifier struct {
	PublicKeyPEM string
	// Algorithm, when set, must match the token header alg.
	Algorithm string
}

// Verify implements SignatureVerifier.
func (v PEMVerifier) Verify(headerSegment, payloadSegment string, signature []byte, alg string) error {
	if strings.TrimSpace(v.PublicKeyPEM) == "" {
		return fmt.Errorf("%w: public key pem required", ErrMissingKey)
	}
	if v.Algorithm != "" && v.Algorithm != alg {
		return fmt.Errorf("%w: alg %q does not match expected %q", ErrSignatureInvalid, alg, v.Algorithm)
	}
	pub, err := parsePublicKeyPEM(v.PublicKeyPEM)
	if err != nil {
		return err
	}
	signingInput := []byte(headerSegment + "." + payloadSegment)
	switch alg {
	case "RS256", "RS384", "RS512":
		rsaKey, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: RSA key required for %s", ErrSignatureInvalid, alg)
		}
		sum := digest(alg, signingInput)
		if err := rsa.VerifyPKCS1v15(rsaKey, hashCrypto(alg), sum, signature); err != nil {
			return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
		}
		return nil
	case "ES256", "ES384", "ES512":
		ecKey, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: ECDSA key required for %s", ErrSignatureInvalid, alg)
		}
		sum := digest(alg, signingInput)
		r, s, err := parseECDSASignature(signature, CurveByteLen(ecKey))
		if err != nil {
			return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
		}
		if !ecdsa.Verify(ecKey, sum, r, s) {
			return fmt.Errorf("%w: ecdsa verify failed", ErrSignatureInvalid)
		}
		return nil
	case "none":
		return fmt.Errorf("%w: alg none is not allowed", ErrSignatureInvalid)
	default:
		return fmt.Errorf("%w: unsupported alg %q", ErrSignatureInvalid, alg)
	}
}

func digest(alg string, data []byte) []byte {
	h := hashForAlg(alg)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func parsePublicKeyPEM(pemData string) (any, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("%w: invalid pem", ErrMissingKey)
	}
	switch block.Type {
	case "PUBLIC KEY":
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMissingKey, err)
		}
		return pub, nil
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMissingKey, err)
		}
		return cert.PublicKey, nil
	case "RSA PUBLIC KEY":
		pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMissingKey, err)
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("%w: unsupported pem type %q", ErrMissingKey, block.Type)
	}
}

func hashForAlg(alg string) hash.Hash {
	switch alg {
	case "RS256", "ES256":
		return sha256.New()
	case "RS384", "ES384":
		return sha512.New384()
	case "RS512", "ES512":
		return sha512.New()
	default:
		return sha256.New()
	}
}

func hashCrypto(alg string) crypto.Hash {
	switch alg {
	case "RS256", "ES256":
		return crypto.SHA256
	case "RS384", "ES384":
		return crypto.SHA384
	case "RS512", "ES512":
		return crypto.SHA512
	default:
		return crypto.SHA256
	}
}

// CurveByteLen returns the ECDSA signature coordinate length for a public key.
func CurveByteLen(key *ecdsa.PublicKey) int {
	return (key.Curve.Params().BitSize + 7) / 8
}

func parseECDSASignature(sig []byte, coordLen int) (*big.Int, *big.Int, error) {
	if len(sig) != 2*coordLen {
		return nil, nil, fmt.Errorf("unexpected ecdsa signature length %d want %d", len(sig), 2*coordLen)
	}
	r := new(big.Int).SetBytes(sig[:coordLen])
	s := new(big.Int).SetBytes(sig[coordLen:])
	return r, s, nil
}
