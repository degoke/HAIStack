package store

import (
	"context"
	"database/sql"
)

type SQLDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (sqlTx, error)
}

type sqlTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	Commit() error
	Rollback() error
}

type dialectDB struct {
	db *sql.DB
	d  Dialect
}

// WrapSQLDB wraps database/sql with dialect-aware placeholder rebinding.
func WrapSQLDB(db *sql.DB, d Dialect) SQLDB {
	if db == nil {
		return nil
	}
	return dialectDB{db: db, d: d}
}

func wrapSQLDB(db *sql.DB, d Dialect) SQLDB {
	return WrapSQLDB(db, d)
}

func (d dialectDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, rebindQuery(d.d, query), args...)
}

func (d dialectDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, rebindQuery(d.d, query), args...)
}

func (d dialectDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, rebindQuery(d.d, query), args...)
}

func (d dialectDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (sqlTx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return dialectTx{tx: tx, d: d.d}, nil
}

type dialectTx struct {
	tx *sql.Tx
	d  Dialect
}

func (t dialectTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, rebindQuery(t.d, query), args...)
}

func (t dialectTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebindQuery(t.d, query), args...)
}

func (t dialectTx) Commit() error   { return t.tx.Commit() }
func (t dialectTx) Rollback() error { return t.tx.Rollback() }
