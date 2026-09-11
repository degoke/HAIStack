package store

import "strings"

// Dialect selects SQL placeholder and upsert syntax for a persistence backend.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

func (d Dialect) placeholder(index int) string {
	switch d {
	case DialectPostgres:
		return "$" + itoa(index)
	default:
		return "?"
	}
}

func rebindQuery(d Dialect, query string) string {
	if d != DialectPostgres {
		return query
	}
	var b strings.Builder
	index := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteString(d.placeholder(index))
			index++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func insertIgnoreSigningKeyQuery(d Dialect) string {
	switch d {
	case DialectPostgres:
		return `
		INSERT INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (issuer, key_id) DO NOTHING`
	default:
		return `
		INSERT OR IGNORE INTO hai_oauth_signing_key (
			issuer, key_id, private_key_pem, encryption_nonce, active, created_at, retired_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`
	}
}

func upsertRevokedTokenQuery(d Dialect) string {
	switch d {
	case DialectPostgres:
		return `
		INSERT INTO hai_oauth_revoked_token (token_id, token_type, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (token_id) DO UPDATE SET
			token_type = EXCLUDED.token_type,
			expires_at = EXCLUDED.expires_at`
	default:
		return `
		INSERT OR REPLACE INTO hai_oauth_revoked_token (token_id, token_type, expires_at)
		VALUES (?, ?, ?)`
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
