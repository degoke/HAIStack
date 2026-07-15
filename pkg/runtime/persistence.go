package runtime

import (
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

// Persistence exposes narrow read-oriented access to underlying storage handles.
// It is intended for operator tooling such as the haistack CLI.
type Persistence struct {
	SQLite   *sqlite.DB
	Postgres *postgres.DB
	TenantDB *postgres.TenantDB
}

// Persistence returns the wired persistence backends for the runtime, if any.
func (rt *Runtime) Persistence() Persistence {
	var tenantDB *postgres.TenantDB
	if rt.services != nil {
		tenantDB = rt.services.TenantDB
	}
	return Persistence{
		SQLite:   rt.sqliteDB,
		Postgres: rt.postgresDB,
		TenantDB: tenantDB,
	}
}
