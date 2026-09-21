package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultOAuthSigningKeyID = "haistack"

// SigningKeyOptions configures persisted OAuth signing keys.
type SigningKeyOptions struct {
	ActiveKeyID      string
	EncryptionSecret string
	RotateOnStartup  bool
	// VerificationTTL is how long a rotated key stays in JWKS (default: 1 hour,
	// matching AccessTokenTTL). After that, retired_at is in the past and the kid is dropped.
	VerificationTTL time.Duration
}

// SigningKeySet holds the active signer and verification keys for JWKS.
type SigningKeySet struct {
	Active         *oauth.KeySet
	Verification   []*oauth.KeySet
	EncryptionUsed bool
}

// LoadOrCreateSQLiteSigningKeySet loads or creates signing keys for issuer in SQLite.
func LoadOrCreateSQLiteSigningKeySet(db *sql.DB, issuer string, opts SigningKeyOptions) (SigningKeySet, error) {
	if db == nil {
		return SigningKeySet{}, fmt.Errorf("%w: db is nil", oauth.ErrInvalidConfig)
	}
	issuer = trimOAuthIssuer(issuer)
	secret := signingSecret(opts)
	keyID := activeKeyID(opts)
	ttl := verificationTTL(opts)
	if opts.RotateOnStartup {
		if err := rotateSQLiteSigningKey(db, issuer, keyID, secret, ttl); err != nil {
			return SigningKeySet{}, err
		}
	}
	if err := stampInactiveSQLiteSigningKeys(db, issuer, ttl); err != nil {
		return SigningKeySet{}, err
	}
	set, err := loadSQLiteSigningKeySet(db, issuer, secret)
	if err == nil && set.Active != nil {
		return set, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SigningKeySet{}, err
	}
	if err := insertSQLiteSigningKey(db, issuer, keyID, secret); err != nil {
		return SigningKeySet{}, err
	}
	return loadSQLiteSigningKeySet(db, issuer, secret)
}

// LoadOrCreatePostgresSigningKeySet loads or creates signing keys for issuer in Postgres.
func LoadOrCreatePostgresSigningKeySet(pool *pgxpool.Pool, issuer string, opts SigningKeyOptions) (SigningKeySet, error) {
	if pool == nil {
		return SigningKeySet{}, fmt.Errorf("%w: pool is nil", oauth.ErrInvalidConfig)
	}
	issuer = trimOAuthIssuer(issuer)
	secret := signingSecret(opts)
	keyID := activeKeyID(opts)
	ttl := verificationTTL(opts)
	if opts.RotateOnStartup {
		if err := rotatePostgresSigningKey(pool, issuer, keyID, secret, ttl); err != nil {
			return SigningKeySet{}, err
		}
	}
	if err := stampInactivePostgresSigningKeys(pool, issuer, ttl); err != nil {
		return SigningKeySet{}, err
	}
	set, err := loadPostgresSigningKeySet(pool, issuer, secret)
	if err == nil && set.Active != nil {
		return set, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SigningKeySet{}, err
	}
	if err := insertPostgresSigningKey(pool, issuer, keyID, secret); err != nil {
		return SigningKeySet{}, err
	}
	return loadPostgresSigningKeySet(pool, issuer, secret)
}

func signingSecret(opts SigningKeyOptions) string {
	secret := strings.TrimSpace(opts.EncryptionSecret)
	if secret == "" {
		secret = oauth.SigningKeyEncryptionSecret()
	}
	return secret
}

func verificationTTL(opts SigningKeyOptions) time.Duration {
	if opts.VerificationTTL > 0 {
		return opts.VerificationTTL
	}
	return time.Hour
}

func activeKeyID(opts SigningKeyOptions) string {
	keyID := strings.TrimSpace(opts.ActiveKeyID)
	if keyID == "" {
		keyID = defaultOAuthSigningKeyID
	}
	return keyID
}

func loadSQLiteSigningKeySet(db *sql.DB, issuer, secret string) (SigningKeySet, error) {
	rows, err := db.QueryContext(context.Background(), `
		SELECT key_id, private_key_pem, encryption_nonce, active, retired_at
		FROM hai_oauth_signing_key
		WHERE issuer = ?
		ORDER BY active DESC, created_at DESC`, issuer)
	if err != nil {
		return SigningKeySet{}, err
	}
	defer func() { _ = rows.Close() }()
	return scanSigningKeyRows(rows, secret)
}

func loadPostgresSigningKeySet(pool *pgxpool.Pool, issuer, secret string) (SigningKeySet, error) {
	rows, err := pool.Query(context.Background(), `
		SELECT key_id, private_key_pem, encryption_nonce, active, retired_at
		FROM hai_oauth_signing_key
		WHERE issuer = $1
		ORDER BY active DESC, created_at DESC`, issuer)
	if err != nil {
		return SigningKeySet{}, err
	}
	defer rows.Close()
	return scanSigningKeyRows(rows, secret)
}

type signingKeyRowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanSigningKeyRows(rows signingKeyRowScanner, secret string) (SigningKeySet, error) {
	now := time.Now()
	var set SigningKeySet
	for rows.Next() {
		var keyID, pemRaw, nonce, retiredAt string
		var active int
		if err := rows.Scan(&keyID, &pemRaw, &nonce, &active, &retiredAt); err != nil {
			return SigningKeySet{}, err
		}
		keySet, err := decodeStoredKeySet(keyID, pemRaw, nonce, secret)
		if err != nil {
			return SigningKeySet{}, err
		}
		if strings.TrimSpace(nonce) != "" {
			set.EncryptionUsed = true
		}
		if retiredAt = strings.TrimSpace(retiredAt); retiredAt != "" {
			t, err := parseOAuthExpiry(retiredAt)
			if err != nil {
				continue
			}
			keySet.RetireAt = t
		}
		if active != 0 && keySet.Published(now) {
			set.Active = keySet
		}
		if keySet.Published(now) {
			set.Verification = append(set.Verification, keySet)
		}
	}
	if err := rows.Err(); err != nil {
		return SigningKeySet{}, err
	}
	if set.Active == nil {
		return SigningKeySet{}, sql.ErrNoRows
	}
	return set, nil
}

func rotateSQLiteSigningKey(db *sql.DB, issuer, keyID, secret string, ttl time.Duration) error {
	newID := keyID + "-" + randomSigningKeySuffix()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()
	retiredAt := formatOAuthTime(now.Add(ttl))
	if _, err := tx.ExecContext(context.Background(), `
		UPDATE hai_oauth_signing_key
		SET active = 0, retired_at = ?
		WHERE issuer = ? AND active = 1 AND retired_at = ''`, retiredAt, issuer); err != nil {
		return err
	}
	keySet, pemRaw, nonce, err := generateStoredKeySet(newID, secret)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES (?, ?, ?, ?, 1, ?, '')`,
		issuer, keySet.KeyID, pemRaw, nonce, formatOAuthTime(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func rotatePostgresSigningKey(pool *pgxpool.Pool, issuer, keyID, secret string, ttl time.Duration) error {
	newID := keyID + "-" + randomSigningKeySuffix()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := time.Now()
	retiredAt := formatOAuthTime(now.Add(ttl))
	if _, err := tx.Exec(context.Background(), `
		UPDATE hai_oauth_signing_key
		SET active = 0, retired_at = $1
		WHERE issuer = $2 AND active = 1 AND retired_at = ''`, retiredAt, issuer); err != nil {
		return err
	}
	keySet, pemRaw, nonce, err := generateStoredKeySet(newID, secret)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES ($1, $2, $3, $4, 1, $5, '')`,
		issuer, keySet.KeyID, pemRaw, nonce, formatOAuthTime(now)); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func stampInactiveSQLiteSigningKeys(db *sql.DB, issuer string, ttl time.Duration) error {
	retiredAt := formatOAuthTime(time.Now().Add(ttl))
	_, err := db.ExecContext(context.Background(), `
		UPDATE hai_oauth_signing_key
		SET retired_at = ?
		WHERE issuer = ? AND active = 0 AND retired_at = ''`, retiredAt, issuer)
	return err
}

func stampInactivePostgresSigningKeys(pool *pgxpool.Pool, issuer string, ttl time.Duration) error {
	retiredAt := formatOAuthTime(time.Now().Add(ttl))
	_, err := pool.Exec(context.Background(), `
		UPDATE hai_oauth_signing_key
		SET retired_at = $1
		WHERE issuer = $2 AND active = 0 AND retired_at = ''`, retiredAt, issuer)
	return err
}

func randomSigningKeySuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "rot"
	}
	return fmt.Sprintf("%x", b)
}

func insertSQLiteSigningKey(db *sql.DB, issuer, keyID, secret string) error {
	keySet, pemRaw, nonce, err := generateStoredKeySet(keyID, secret)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT OR IGNORE INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES (?, ?, ?, ?, 1, ?, '')`,
		issuer, keySet.KeyID, pemRaw, nonce, formatOAuthTime(time.Now()))
	return err
}

func insertPostgresSigningKey(pool *pgxpool.Pool, issuer, keyID, secret string) error {
	keySet, pemRaw, nonce, err := generateStoredKeySet(keyID, secret)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES ($1, $2, $3, $4, 1, $5, '')
		ON CONFLICT (issuer, key_id) DO NOTHING`,
		issuer, keySet.KeyID, pemRaw, nonce, formatOAuthTime(time.Now()))
	return err
}

func generateStoredKeySet(keyID, secret string) (*oauth.KeySet, string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", "", fmt.Errorf("oauth signing key: %w", err)
	}
	if strings.TrimSpace(keyID) == "" {
		keyID = defaultOAuthSigningKeyID
	}
	keySet := &oauth.KeySet{PrivateKey: key, KeyID: keyID, Algorithm: "RS256"}
	pemBytes, err := marshalRSAPrivateKeyPEM(key)
	if err != nil {
		return nil, "", "", err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return keySet, string(pemBytes), "", nil
	}
	ciphertext, nonce, err := oauth.EncryptSigningKeyPEM(pemBytes, secret)
	if err != nil {
		return nil, "", "", err
	}
	return keySet, ciphertext, nonce, nil
}

func decodeStoredKeySet(keyID, pemRaw, nonce, secret string) (*oauth.KeySet, error) {
	raw := []byte(pemRaw)
	if strings.TrimSpace(nonce) != "" {
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("%w: encrypted signing key requires OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", oauth.ErrInvalidConfig)
		}
		decrypted, err := oauth.DecryptSigningKeyPEM(pemRaw, nonce, secret)
		if err != nil {
			return nil, err
		}
		raw = decrypted
	}
	key, err := parseRSAPrivateKeyPEM(string(raw))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keyID) == "" {
		keyID = defaultOAuthSigningKeyID
	}
	return &oauth.KeySet{PrivateKey: key, KeyID: keyID, Algorithm: "RS256"}, nil
}

func trimOAuthIssuer(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return ""
	}
	return strings.TrimRight(issuer, "/")
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
