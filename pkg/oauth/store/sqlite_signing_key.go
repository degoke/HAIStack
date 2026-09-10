package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

const defaultOAuthSigningKeyID = "haistack"

// LoadOrCreateRS256Signer loads a persisted RS256 signer for issuer or creates one on first use.
// The private key is stored in SQLite as PKCS#8 PEM. Concurrent first-start races resolve by
// reloading the row after INSERT OR IGNORE.
func LoadOrCreateRS256Signer(db *sql.DB, issuer, keyID string) (oauth.RS256Signer, error) {
	if db == nil {
		return oauth.RS256Signer{}, fmt.Errorf("%w: db is nil", oauth.ErrInvalidConfig)
	}
	issuer = trimIssuer(issuer)
	if issuer == "" {
		return oauth.RS256Signer{}, fmt.Errorf("%w: issuer required", oauth.ErrInvalidConfig)
	}
	if keyID == "" {
		keyID = defaultOAuthSigningKeyID
	}

	signer, err := loadRS256Signer(db, issuer)
	if err == nil {
		return signer, nil
	}
	if err != sql.ErrNoRows {
		return oauth.RS256Signer{}, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return oauth.RS256Signer{}, fmt.Errorf("oauth signing key: %w", err)
	}
	pemBytes, err := marshalRSAPrivateKeyPEM(key)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT OR IGNORE INTO hai_oauth_signing_key (issuer, key_id, private_key_pem, created_at)
		VALUES (?, ?, ?, ?)`,
		issuer, keyID, string(pemBytes), formatTime(time.Now()),
	)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	return loadRS256Signer(db, issuer)
}

func loadRS256Signer(db *sql.DB, issuer string) (oauth.RS256Signer, error) {
	var keyID, pemRaw string
	err := db.QueryRowContext(context.Background(), `
		SELECT key_id, private_key_pem FROM hai_oauth_signing_key WHERE issuer = ?`, issuer).
		Scan(&keyID, &pemRaw)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	key, err := parseRSAPrivateKeyPEM(pemRaw)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	if keyID == "" {
		keyID = defaultOAuthSigningKeyID
	}
	return oauth.RS256Signer{PrivateKey: key, Kid: keyID}, nil
}

func trimIssuer(issuer string) string {
	for len(issuer) > 0 && issuer[len(issuer)-1] == '/' {
		issuer = issuer[:len(issuer)-1]
	}
	return issuer
}

func marshalRSAPrivateKeyPEM(key *rsa.PrivateKey) ([]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("%w: rsa private key required", oauth.ErrInvalidConfig)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func parseRSAPrivateKeyPEM(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("%w: invalid signing key pem", oauth.ErrInvalidConfig)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: signing key must be rsa", oauth.ErrInvalidConfig)
	}
	return key, nil
}
