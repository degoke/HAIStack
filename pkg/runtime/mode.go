package runtime

// Mode identifies the effective runtime deployment shape inferred from builder selections.
type Mode string

const (
	// ModeLocalSQLite uses embedded SQLite as the only durable store.
	ModeLocalSQLite Mode = "local-sqlite"

	// ModeEdgePostgresAllInOne uses one Postgres tenant for resources, history,
	// search, jobs, modules, audit, blobs, and sync/event stores.
	ModeEdgePostgresAllInOne Mode = "edge-postgres-all-in-one"

	// ModeCloudPostgresPlusExternalServices uses Postgres for canonical resource,
	// history, event, and module state with explicit external service adapters.
	ModeCloudPostgresPlusExternalServices Mode = "cloud-postgres-plus-external-services"
)

func (m Mode) String() string {
	return string(m)
}
