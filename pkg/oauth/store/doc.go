// Package store provides durable OAuth authorization state backed by Postgres or SQLite.
//
// Use ApplyPostgresStores or ApplySQLiteStores to wire an oauth.Config, then construct
// the server with oauth.NewServer or store.NewServer.
package store
