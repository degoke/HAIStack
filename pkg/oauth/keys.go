package oauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// KeySet holds signing keys exposed through JWKS.
type KeySet struct {
	PrivateKey *rsa.PrivateKey
	KeyID      string
	Algorithm  string
}

// LoadKeySetFromPEM loads an RSA signing key from a PKCS#8 or PKCS#1 PEM file.
func LoadKeySetFromPEM(path string, keyID string) (*KeySet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oauth: read signing key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("oauth: invalid signing key PEM")
	}
	var privateKey *rsa.PrivateKey
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("oauth: signing key is not RSA")
		}
		privateKey = rsaKey
	} else if rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		privateKey = rsaKey
	} else {
		return nil, fmt.Errorf("oauth: parse signing key: %w", err)
	}
	if strings.TrimSpace(keyID) == "" {
		keyID = randomKeyID()
	}
	return &KeySet{PrivateKey: privateKey, KeyID: keyID, Algorithm: "RS256"}, nil
}

// NewKeySet generates an RSA signing key set.
func NewKeySet(bits int) (*KeySet, error) {
	if bits <= 0 {
		bits = 2048
	}
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, err
	}
	return &KeySet{
		PrivateKey: key,
		KeyID:      randomKeyID(),
		Algorithm:  "RS256",
	}, nil
}

// PublicKeyPEM returns PKIX PEM for the public key.
func (k *KeySet) PublicKeyPEM() (string, error) {
	if k == nil || k.PrivateKey == nil {
		return "", fmt.Errorf("oauth: key set is nil")
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PrivateKey.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// JWKS returns a JWKS document for the public key.
func (k *KeySet) JWKS() map[string]any {
	pub := k.PrivateKey.PublicKey
	n := base64URLEncode(pub.N.Bytes())
	e := base64URLEncode(big.NewInt(int64(pub.E)).Bytes())
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": k.KeyID,
			"use": "sig",
			"alg": k.Algorithm,
			"n":   n,
			"e":   e,
		}},
	}
}

func randomKeyID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "key-1"
	}
	return base64URLEncode(b)
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
