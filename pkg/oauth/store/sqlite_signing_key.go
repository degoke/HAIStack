package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

const defaultOAuthSigningKeyID = "haistack"

// SigningKeyOptions configures persisted OAuth signing keys.
type SigningKeyOptions struct {
	ActiveKeyID        string
	EncryptionSecret   string
	RotateOnStartup    bool
}

// SigningKeySet holds the active signer and all verification keys for JWKS.
type SigningKeySet struct {
	Active oauth.RS256Signer
	JWKS   []oauth.RS256Signer
}

// LoadOrCreateSigningKeySet loads or creates the active signing key set for issuer.
func LoadOrCreateSigningKeySet(db *sql.DB, issuer string, opts SigningKeyOptions) (SigningKeySet, error) {
	if db == nil {
		return SigningKeySet{}, fmt.Errorf("%w: db is nil", oauth.ErrInvalidConfig)
	}
	issuer = trimIssuer(issuer)
	if issuer == "" {
		return SigningKeySet{}, fmt.Errorf("%w: issuer required", oauth.ErrInvalidConfig)
	}
	keyID := strings.TrimSpace(opts.ActiveKeyID)
	if keyID == "" {
		keyID = defaultOAuthSigningKeyID
	}
	secret := strings.TrimSpace(opts.EncryptionSecret)
	if secret == "" {
		secret = oauth.SigningKeyEncryptionSecret()
	}
	if opts.RotateOnStartup {
		if _, err := rotateSigningKey(db, issuer, keyID, secret); err != nil {
			return SigningKeySet{}, err
		}
	}
	set, err := loadSigningKeySet(db, issuer, secret)
	if err == nil && set.Active.PrivateKey != nil {
		return set, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return SigningKeySet{}, err
	}
	if err := insertSigningKey(db, issuer, keyID, secret, true); err != nil {
		return SigningKeySet{}, err
	}
	return loadSigningKeySet(db, issuer, secret)
}

// LoadOrCreateRS256Signer loads the active RS256 signer for issuer.
func LoadOrCreateRS256Signer(db *sql.DB, issuer, keyID string) (oauth.RS256Signer, error) {
	set, err := LoadOrCreateSigningKeySet(db, issuer, SigningKeyOptions{ActiveKeyID: keyID})
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	return set.Active, nil
}

func loadSigningKeySet(db *sql.DB, issuer, secret string) (SigningKeySet, error) {
	rows, err := db.QueryContext(context.Background(), `
		SELECT key_id, private_key_pem, encryption_nonce, active, retired_at
		FROM hai_oauth_signing_key
		WHERE issuer = ?
		ORDER BY active DESC, created_at DESC`, issuer)
	if err != nil {
		return SigningKeySet{}, err
	}
	defer func() { _ = rows.Close() }()

	var set SigningKeySet
	var jwks []oauth.RS256Signer
	for rows.Next() {
		var keyID, pemRaw, nonce, retiredAt string
		var active int
		if err := rows.Scan(&keyID, &pemRaw, &nonce, &active, &retiredAt); err != nil {
			return SigningKeySet{}, err
		}
		signer, err := decodeStoredSigner(keyID, pemRaw, nonce, secret)
		if err != nil {
			return SigningKeySet{}, err
		}
		if active != 0 && retiredAt == "" {
			set.Active = signer
		}
		if retiredAt == "" {
			jwks = append(jwks, signer)
		}
	}
	if err := rows.Err(); err != nil {
		return SigningKeySet{}, err
	}
	if set.Active.PrivateKey == nil {
		return SigningKeySet{}, sql.ErrNoRows
	}
	set.JWKS = jwks
	return set, nil
}

func rotateSigningKey(db *sql.DB, issuer, keyID, secret string) (oauth.RS256Signer, error) {
	newID := keyID
	if token, err := oauth.NewRandomToken(); err == nil && token != "" {
		newID = keyID + "-" + token[:8]
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := formatTime(time.Now())
	if _, err := tx.ExecContext(context.Background(), `
		UPDATE hai_oauth_signing_key
		SET active = 0, retired_at = ?
		WHERE issuer = ? AND active = 1 AND retired_at = ''`, now, issuer); err != nil {
		return oauth.RS256Signer{}, err
	}
	signer, err := generateStoredSigner(newID)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	pemRaw, nonce, err := encodeStoredSigner(signer.PrivateKey, secret)
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES (?, ?, ?, ?, 1, ?, '')`,
		issuer, signer.Kid, pemRaw, nonce, now); err != nil {
		return oauth.RS256Signer{}, err
	}
	if err := tx.Commit(); err != nil {
		return oauth.RS256Signer{}, err
	}
	return signer, nil
}

func insertSigningKey(db *sql.DB, issuer, keyID, secret string, active bool) error {
	signer, err := generateStoredSigner(keyID)
	if err != nil {
		return err
	}
	pemRaw, nonce, err := encodeStoredSigner(signer.PrivateKey, secret)
	if err != nil {
		return err
	}
	activeInt := 0
	if active {
		activeInt = 1
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT OR IGNORE INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES (?, ?, ?, ?, ?, ?, '')`,
		issuer, signer.Kid, pemRaw, nonce, activeInt, formatTime(time.Now()),
	)
	return err
}

func generateStoredSigner(keyID string) (oauth.RS256Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return oauth.RS256Signer{}, fmt.Errorf("oauth signing key: %w", err)
	}
	if strings.TrimSpace(keyID) == "" {
		keyID = defaultOAuthSigningKeyID
	}
	return oauth.RS256Signer{PrivateKey: key, Kid: keyID}, nil
}

func encodeStoredSigner(key *rsa.PrivateKey, secret string) (pemRaw, nonce string, err error) {
	pemBytes, err := marshalRSAPrivateKeyPEM(key)
	if err != nil {
		return "", "", err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return string(pemBytes), "", nil
	}
	return oauth.EncryptSigningKeyPEM(pemBytes, secret)
}

func decodeStoredSigner(keyID, pemRaw, nonce, secret string) (oauth.RS256Signer, error) {
	secret = strings.TrimSpace(secret)
	raw := []byte(pemRaw)
	if strings.TrimSpace(nonce) != "" {
		if secret == "" {
			return oauth.RS256Signer{}, fmt.Errorf("%w: encrypted signing key requires %s", oauth.ErrInvalidConfig, "OAUTH_SIGNING_KEY_ENCRYPTION_SECRET")
		}
		decrypted, err := oauth.DecryptSigningKeyPEM(pemRaw, nonce, secret)
		if err != nil {
			return oauth.RS256Signer{}, err
		}
		raw = decrypted
	}
	key, err := parseRSAPrivateKeyPEM(string(raw))
	if err != nil {
		return oauth.RS256Signer{}, err
	}
	if strings.TrimSpace(keyID) == "" {
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
