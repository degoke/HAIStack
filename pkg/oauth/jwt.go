package oauth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func signJWT(unsigned string, key *rsa.PrivateKey, algorithm string) (string, error) {
	if algorithm == "" {
		algorithm = "RS256"
	}
	hash := sha256.Sum256([]byte(unsigned))
	var sig []byte
	var err error
	switch algorithm {
	case "RS256", "":
		sig, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	default:
		return "", fmt.Errorf("unsupported algorithm %q", algorithm)
	}
	if err != nil {
		return "", err
	}
	return base64URLEncode(sig), nil
}

func buildJWT(header map[string]string, payload map[string]any, key *rsa.PrivateKey) (string, error) {
	algorithm := header["alg"]
	if algorithm == "" {
		algorithm = "RS256"
		header["alg"] = algorithm
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	unsigned := base64URLEncode(headerJSON) + "." + base64URLEncode(payloadJSON)
	sig, err := signJWT(unsigned, key, algorithm)
	if err != nil {
		return "", err
	}
	return unsigned + "." + sig, nil
}
