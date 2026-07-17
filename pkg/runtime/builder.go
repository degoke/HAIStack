package runtime

import (
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

// Builder composes a runtime from storage backends and optional capabilities.
type Builder struct {
	sqlitePath  string
	postgresDSN string
	tenantID    string

	blobStore      BlobStoreAdapter
	externalSearch ExternalSearchAdapter
	warehouse      WarehouseAdapter

	fhirPathEngine fhirpath.Engine
	sdcService     hahttp.SDCService
	searchEnabled  bool

	syncHubURL string
	syncHub    hasync.Hub
	syncNodeID string

	modulePaths []string
	httpAddr    string
}

// New returns a new runtime builder.
func New() *Builder {
	return &Builder{}
}

// WithSQLite selects embedded SQLite at the given database path.
func (b *Builder) WithSQLite(path string) *Builder {
	b.sqlitePath = path
	return b
}

// WithPostgresAllInOne selects a single-tenant Postgres deployment.
func (b *Builder) WithPostgresAllInOne(dsn, tenantID string) *Builder {
	b.postgresDSN = dsn
	b.tenantID = tenantID
	return b
}

// WithExternalBlobStore registers an external blob store adapter for cloud mode.
func (b *Builder) WithExternalBlobStore(store BlobStoreAdapter) *Builder {
	b.blobStore = store
	return b
}

// WithExternalSearch registers an external search adapter for cloud mode.
func (b *Builder) WithExternalSearch(search ExternalSearchAdapter) *Builder {
	b.externalSearch = search
	return b
}

// WithExternalWarehouse registers an external warehouse adapter for cloud mode.
func (b *Builder) WithExternalWarehouse(warehouse WarehouseAdapter) *Builder {
	b.warehouse = warehouse
	return b
}

// WithFHIRPath supplies a FHIRPath engine; when omitted, one is created at build time.
func (b *Builder) WithFHIRPath(engine fhirpath.Engine) *Builder {
	b.fhirPathEngine = engine
	return b
}

// WithSDC supplies the HTTP SDC operation adapter. When omitted, runtime
// wires the default core/FHIRPath adapter; applications can inject extractors,
// terminology, and adaptive session policy through this seam.
func (b *Builder) WithSDC(service hahttp.SDCService) *Builder { b.sdcService = service; return b }

// WithSearch enables search indexing and query execution.
// On SQLite this wires embedded/basic search; advanced search remains Postgres-first.
func (b *Builder) WithSearch() *Builder {
	b.searchEnabled = true
	return b
}

// WithSync enables device sync against a remote hub URL.
func (b *Builder) WithSync(hubURL string) *Builder {
	b.syncHubURL = hubURL
	return b
}

// WithSyncHub enables device sync using a pre-built hub adapter.
func (b *Builder) WithSyncHub(hub hasync.Hub) *Builder {
	b.syncHub = hub
	return b
}

// WithSyncNode sets the device node ID used by the sync engine.
// When omitted, a default node ID is assigned at build time.
func (b *Builder) WithSyncNode(nodeID string) *Builder {
	b.syncNodeID = nodeID
	return b
}

// WithModules installs modules from local filesystem directories at build time.
func (b *Builder) WithModules(paths ...string) *Builder {
	b.modulePaths = append(b.modulePaths, paths...)
	return b
}

// WithHTTP sets the listen address for a managed HTTP server started by Runtime.Start.
func (b *Builder) WithHTTP(addr string) *Builder {
	b.httpAddr = addr
	return b
}

// resolveMode infers the effective mode from builder selections.
func (b *Builder) resolveMode() (Mode, error) {
	hasSQLite := b.sqlitePath != ""
	hasPostgres := b.postgresDSN != ""
	hasExternal := b.blobStore != nil || b.externalSearch != nil || b.warehouse != nil

	switch {
	case hasSQLite && hasPostgres:
		return "", ErrConflictingStorage
	case !hasSQLite && !hasPostgres:
		return "", ErrNoStorage
	case hasSQLite:
		if hasExternal {
			return "", ErrExternalAdapterInSQLite
		}
		return ModeLocalSQLite, nil
	case hasPostgres:
		if b.tenantID == "" {
			return "", ErrMissingTenantID
		}
		if hasExternal {
			return ModeCloudPostgresPlusExternalServices, nil
		}
		return ModeEdgePostgresAllInOne, nil
	default:
		return "", ErrNoStorage
	}
}

// normalizedConfig builds the public Config from builder state.
func (b *Builder) normalizedConfig(mode Mode) Config {
	syncEnabled := b.syncHubURL != "" || b.syncHub != nil
	nodeID := b.syncNodeID
	if nodeID == "" {
		nodeID = "runtime-node"
	}
	return Config{
		Mode:          mode,
		SQLitePath:    b.sqlitePath,
		PostgresDSN:   b.postgresDSN,
		TenantID:      b.tenantID,
		SearchEnabled: b.searchEnabled,
		ModulePaths:   append([]string(nil), b.modulePaths...),
		HTTPAddr:      b.httpAddr,
		SyncEnabled:   syncEnabled,
		SyncHubURL:    b.syncHubURL,
		SyncNodeID:    nodeID,
	}
}
