package runtime

// Config is the normalized runtime configuration produced by Builder.Build.
type Config struct {
	Mode Mode

	// Storage
	SQLitePath  string
	PostgresDSN string
	TenantID    string

	// Capabilities
	SearchEnabled bool
	ModulePaths   []string
	HTTPAddr      string

	// Sync
	SyncEnabled bool
	SyncHubURL  string
	SyncNodeID  string
}
