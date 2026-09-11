package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

const signingKeyEncryptionEnv = "OAUTH_SIGNING_KEY_ENCRYPTION_SECRET"

// SigningKeyEncryptionSecret returns the configured signing-key encryption secret.
func SigningKeyEncryptionSecret() string {
	return strings.TrimSpace(os.Getenv(signingKeyEncryptionEnv))
}

// RequireSigningKeyEncryptionSecret returns an error when secret is empty.
func RequireSigningKeyEncryptionSecret() error {
	if SigningKeyEncryptionSecret() == "" {
		return fmt.Errorf("%w: set %s for production signing key storage", ErrInvalidConfig, signingKeyEncryptionEnv)
	}
	return nil
}

func deriveSigningKeyEncryptionKey(secret string) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("%w: signing key encryption secret required", ErrInvalidConfig)
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:], nil
}

func EncryptSigningKeyPEM(pem []byte, secret string) (ciphertext string, nonce string, err error) {
	return encryptSigningKeyPEM(pem, secret)
}

func DecryptSigningKeyPEM(ciphertext, nonce, secret string) ([]byte, error) {
	return decryptSigningKeyPEM(ciphertext, nonce, secret)
}

func encryptSigningKeyPEM(pem []byte, secret string) (ciphertext string, nonce string, err error) {
	key, err := deriveSigningKeyEncryptionKey(secret)
	if err != nil {
		return "", "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", "", err
	}
	sealed := gcm.Seal(nil, iv, pem, nil)
	return base64.StdEncoding.EncodeToString(sealed), base64.StdEncoding.EncodeToString(iv), nil
}

func decryptSigningKeyPEM(ciphertext, nonce, secret string) ([]byte, error) {
	key, err := deriveSigningKeyEncryptionKey(secret)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("decode signing key ciphertext: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(strings.TrimSpace(nonce))
	if err != nil {
		return nil, fmt.Errorf("decode signing key nonce: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, raw, nil)
}
