package runtime

import "errors"

var (
	// ErrNoStorage is returned when neither SQLite nor Postgres is configured.
	ErrNoStorage = errors.New("runtime: exactly one storage backend is required")

	// ErrConflictingStorage is returned when both SQLite and Postgres are configured.
	ErrConflictingStorage = errors.New("runtime: SQLite and Postgres cannot both be configured")

	// ErrMissingTenantID is returned when Postgres is selected without a tenant ID.
	ErrMissingTenantID = errors.New("runtime: tenant ID is required for Postgres modes")

	// ErrExternalAdapterInSQLite is returned when external adapters are used with SQLite.
	ErrExternalAdapterInSQLite = errors.New("runtime: external blob/search/warehouse adapters are not supported in local-sqlite mode")

	// ErrInvalidModeCombination is returned for unsupported dependency combinations.
	ErrInvalidModeCombination = errors.New("runtime: invalid mode and dependency combination")

	// ErrSyncHubRequired is returned when sync is enabled without a hub.
	ErrSyncHubRequired = errors.New("runtime: sync requires WithSync hub URL or WithSyncHub")

	// ErrAlreadyStarted is returned when Start is called on a running runtime.
	ErrAlreadyStarted = errors.New("runtime: already started")

	// ErrNotStarted is returned when operations require a started runtime.
	ErrNotStarted = errors.New("runtime: not started")
)
