package runtime

// Config is the normalized runtime configuration produced by Builder.Build.
type Config struct {
	Mode Mode

	// Storage
	SQLitePath             string
	SQLiteTenantID         string
	SQLiteTerminologyScope string
	PostgresDSN            string
	PostgresSchema         string // empty means the default public schema
	TenantID               string

	// Capabilities
	SearchEnabled bool
	ModulePaths   []string
	HTTPAddr      string

	// Sync
	SyncEnabled bool
	SyncHubURL  string
	SyncNodeID  string
}
