package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// SQLiteStores groups SQLite-backed OAuth persistence stores.
type SQLiteStores struct {
	Codes      oauth.AuthorizationCodeStore
	Refresh    oauth.RefreshTokenStore
	Revocation oauth.TokenRevocationStore
}

// NewSQLiteStores returns OAuth stores backed by the given database.
// Apply pkg/sqlite migration 0012_oauth.sql before use.
func NewSQLiteStores(db *sql.DB) (*SQLiteStores, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is nil", oauth.ErrInvalidConfig)
	}
	return &SQLiteStores{
		Codes:      &SQLiteCodeStore{DB: db},
		Refresh:    &SQLiteRefreshStore{DB: db},
		Revocation: &SQLiteRevocationStore{DB: db},
	}, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, raw)
}

// SQLiteCodeStore persists authorization codes in SQLite.
type SQLiteCodeStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteCodeStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteCodeStore) Issue(entry oauth.AuthCode) (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = s.now().Add(2 * time.Minute)
	}
	_, err = s.DB.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_auth_code (
			code, client_id, redirect_uri, scope, code_challenge, code_challenge_method,
			state, patient, user_id, tenant_hint, expires_at, used
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		code, entry.ClientID, entry.RedirectURI, entry.Scope, entry.CodeChallenge,
		entry.CodeChallengeMethod, entry.State, entry.Patient, entry.User, entry.TenantHint,
		formatTime(entry.ExpiresAt),
	)
	return code, err
}

func (s *SQLiteCodeStore) Exchange(code, clientID, redirectURI, codeVerifier string) (*oauth.AuthCode, error) {
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(context.Background(), `
		SELECT client_id, redirect_uri, scope, code_challenge, code_challenge_method,
		       state, patient, user_id, tenant_hint, expires_at, used
		FROM hai_oauth_auth_code WHERE code = ?`, code)
	var entry oauth.AuthCode
	var expiresRaw string
	var used int
	entry.Code = code
	if err := row.Scan(&entry.ClientID, &entry.RedirectURI, &entry.Scope, &entry.CodeChallenge,
		&entry.CodeChallengeMethod, &entry.State, &entry.Patient, &entry.User, &entry.TenantHint,
		&expiresRaw, &used); err != nil {
		return nil, fmt.Errorf("%w: unknown or used code", oauth.ErrInvalidGrant)
	}
	entry.ExpiresAt, _ = parseTime(expiresRaw)
	if used != 0 {
		return nil, fmt.Errorf("%w: unknown or used code", oauth.ErrInvalidGrant)
	}
	if s.now().After(entry.ExpiresAt) {
		return nil, fmt.Errorf("%w: code expired", oauth.ErrInvalidGrant)
	}
	if entry.ClientID != clientID || entry.RedirectURI != redirectURI {
		return nil, fmt.Errorf("%w: client or redirect mismatch", oauth.ErrInvalidGrant)
	}
	if entry.CodeChallenge != "" {
		if err := verifyPKCE(entry.CodeChallenge, entry.CodeChallengeMethod, codeVerifier); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE hai_oauth_auth_code SET used = 1 WHERE code = ?`, code); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &entry, nil
}

// SQLiteRefreshStore persists refresh tokens in SQLite.
type SQLiteRefreshStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteRefreshStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteRefreshStore) Issue(record oauth.RefreshRecord) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = s.now().Add(30 * 24 * time.Hour)
	}
	_, err = s.DB.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (
			token, client_id, scope, subject, patient, encounter, fhir_user, tenant_hint, expires_at, revoked
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		token, record.ClientID, record.Scope, record.Subject, record.Patient,
		record.Encounter, record.FHIRUser, record.TenantHint, formatTime(record.ExpiresAt),
	)
	return token, err
}

func (s *SQLiteRefreshStore) Lookup(token string) (*oauth.RefreshRecord, error) {
	return s.scanRefresh(token)
}

func (s *SQLiteRefreshStore) Rotate(token, clientID string) (*oauth.RefreshRecord, string, error) {
	record, err := s.scanRefresh(token)
	if err != nil {
		return nil, "", err
	}
	if record.ClientID != clientID {
		return nil, "", fmt.Errorf("%w: client mismatch", oauth.ErrInvalidGrant)
	}
	newToken, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	expires := s.now().Add(30 * 24 * time.Hour)
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(context.Background(), `
		UPDATE hai_oauth_refresh_token SET revoked = 1, replaced_by = ? WHERE token = ?`,
		newToken, token); err != nil {
		return nil, "", err
	}
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_refresh_token (
			token, client_id, scope, subject, patient, encounter, fhir_user, tenant_hint, expires_at, revoked
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		newToken, record.ClientID, record.Scope, record.Subject, record.Patient,
		record.Encounter, record.FHIRUser, record.TenantHint, formatTime(expires),
	); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	replacement := *record
	replacement.Token = newToken
	replacement.ExpiresAt = expires
	return record, newToken, nil
}

func (s *SQLiteRefreshStore) Revoke(token string) error {
	_, err := s.DB.ExecContext(context.Background(), `
		UPDATE hai_oauth_refresh_token SET revoked = 1 WHERE token = ?`, token)
	return err
}

func (s *SQLiteRefreshStore) scanRefresh(token string) (*oauth.RefreshRecord, error) {
	row := s.DB.QueryRowContext(context.Background(), `
		SELECT client_id, scope, subject, patient, encounter, fhir_user, tenant_hint, expires_at, revoked
		FROM hai_oauth_refresh_token WHERE token = ?`, token)
	var record oauth.RefreshRecord
	var expiresRaw string
	var revoked int
	record.Token = token
	if err := row.Scan(&record.ClientID, &record.Scope, &record.Subject, &record.Patient,
		&record.Encounter, &record.FHIRUser, &record.TenantHint, &expiresRaw, &revoked); err != nil {
		return nil, fmt.Errorf("%w: unknown refresh token", oauth.ErrInvalidGrant)
	}
	record.ExpiresAt, _ = parseTime(expiresRaw)
	record.Revoked = revoked != 0
	if record.Revoked {
		return nil, fmt.Errorf("%w: unknown refresh token", oauth.ErrInvalidGrant)
	}
	if s.now().After(record.ExpiresAt) {
		return nil, fmt.Errorf("%w: refresh token expired", oauth.ErrInvalidGrant)
	}
	return &record, nil
}

// SQLiteRevocationStore persists revoked token JTIs in SQLite.
type SQLiteRevocationStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteRevocationStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteRevocationStore) Revoke(tokenID, tokenType string, expiresAt time.Time) error {
	if tokenID == "" {
		return fmt.Errorf("%w: token id required", oauth.ErrInvalidRequest)
	}
	_, err := s.DB.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO hai_oauth_revoked_token (token_id, token_type, expires_at)
		VALUES (?, ?, ?)`, tokenID, tokenType, formatTime(expiresAt))
	return err
}

func (s *SQLiteRevocationStore) IsRevoked(tokenID string) (bool, error) {
	if tokenID == "" {
		return false, nil
	}
	var expiresRaw string
	err := s.DB.QueryRowContext(context.Background(), `
		SELECT expires_at FROM hai_oauth_revoked_token WHERE token_id = ?`, tokenID).Scan(&expiresRaw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	exp, _ := parseTime(expiresRaw)
	if s.now().After(exp) {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM hai_oauth_revoked_token WHERE token_id = ?`, tokenID)
		return false, nil
	}
	return true, nil
}

func randomToken() (string, error) {
	return oauth.NewRandomToken()
}

func verifyPKCE(challenge, method, verifier string) error {
	return oauth.VerifyPKCE(challenge, method, verifier)
}
