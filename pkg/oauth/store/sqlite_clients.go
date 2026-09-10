package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

// SQLiteConsentSessionStore persists consent sessions in SQLite.
type SQLiteConsentSessionStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteConsentSessionStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteConsentSessionStore) Create(issuer string, params url.Values, subject string) (sessionID, csrf string, err error) {
	_, _ = s.PurgeExpired(context.Background())
	sessionID, err = randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err = randomToken()
	if err != nil {
		return "", "", err
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", "", err
	}
	expires := s.now().Add(oauth.DefaultConsentSessionTTL)
	_, err = s.DB.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_consent_session (
			session_id, issuer, params, csrf, subject, expires_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, issuer, string(paramsJSON), csrf, subject, formatTime(expires),
	)
	return sessionID, csrf, err
}

func (s *SQLiteConsentSessionStore) Consume(issuer, sessionID, csrf string) (url.Values, string, error) {
	_, _ = s.PurgeExpired(context.Background())
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback() }()

	var paramsJSON, storedCSRF, subject, expiresRaw string
	var storedIssuer string
	err = tx.QueryRowContext(context.Background(), `
		SELECT issuer, params, csrf, subject, expires_at
		FROM hai_oauth_consent_session WHERE session_id = ?`, sessionID).
		Scan(&storedIssuer, &paramsJSON, &storedCSRF, &subject, &expiresRaw)
	if err != nil {
		return nil, "", oauth.ErrInvalidRequest
	}
	expires, _ := parseTime(expiresRaw)
	if s.now().After(expires) {
		_ = tx.Rollback()
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM hai_oauth_consent_session WHERE session_id = ?`, sessionID)
		return nil, "", oauth.ErrInvalidRequest
	}
	if storedIssuer != issuer || !oauth.TokenEqual(csrf, storedCSRF) {
		return nil, "", oauth.ErrInvalidRequest
	}
	if _, err := tx.ExecContext(context.Background(), `DELETE FROM hai_oauth_consent_session WHERE session_id = ?`, sessionID); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	var params url.Values
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return nil, "", err
	}
	return params, subject, nil
}

func (s *SQLiteConsentSessionStore) PurgeExpired(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM hai_oauth_consent_session WHERE expires_at < ?`, formatTime(s.now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SQLiteClientStore persists OAuth clients in SQLite.
type SQLiteClientStore struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *SQLiteClientStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SQLiteClientStore) Register(issuer string, client oauth.Client) error {
	if err := oauth.ValidateClientRegistration(client); err != nil {
		return err
	}
	var exists int
	err := s.DB.QueryRowContext(context.Background(), `
		SELECT 1 FROM hai_oauth_client WHERE client_id = ? AND issuer = ?`,
		client.ClientID, issuer).Scan(&exists)
	if err == nil {
		return fmt.Errorf("%w: %s", oauth.ErrClientExists, client.ClientID)
	}
	if err != sql.ErrNoRows {
		return err
	}
	return s.upsertClient(issuer, client)
}

func (s *SQLiteClientStore) Upsert(issuer string, client oauth.Client) error {
	if err := oauth.ValidateClientRegistration(client); err != nil {
		return err
	}
	return s.upsertClient(issuer, client)
}

func (s *SQLiteClientStore) upsertClient(issuer string, client oauth.Client) error {
	payload, err := json.Marshal(clientWithoutSecret(client))
	if err != nil {
		return err
	}
	confidential := 0
	if client.Confidential {
		confidential = 1
	}
	storedSecret, err := storedClientSecret(client)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(context.Background(), `
		INSERT INTO hai_oauth_client (
			client_id, issuer, client_json, client_secret, confidential, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, issuer) DO UPDATE SET
			client_json = excluded.client_json,
			client_secret = excluded.client_secret,
			confidential = excluded.confidential`,
		client.ClientID, issuer, string(payload), storedSecret, confidential, formatTime(s.now()),
	)
	return err
}

func storedClientSecret(client oauth.Client) (string, error) {
	if !client.Confidential || client.ClientSecret == "" {
		return "", nil
	}
	return hashClientSecret(client.ClientSecret)
}

func (s *SQLiteClientStore) Lookup(issuer, clientID string) (oauth.Client, error) {
	row := s.DB.QueryRowContext(context.Background(), `
		SELECT client_json, client_secret, confidential
		FROM hai_oauth_client WHERE client_id = ? AND issuer = ?`, clientID, issuer)
	var payload, secret string
	var confidential int
	if err := row.Scan(&payload, &secret, &confidential); err != nil {
		return oauth.Client{}, fmt.Errorf("%w: %s", oauth.ErrInvalidClient, clientID)
	}
	var client oauth.Client
	if err := json.Unmarshal([]byte(payload), &client); err != nil {
		return oauth.Client{}, err
	}
	client.Confidential = confidential != 0
	client.ClientSecretHash = secret
	return client, nil
}

func clientWithoutSecret(client oauth.Client) oauth.Client {
	copy := client
	copy.ClientSecret = ""
	copy.ClientSecretHash = ""
	return copy
}

func hashClientSecret(secret string) (string, error) {
	return oauth.HashClientSecret(secret)
}
