package runtime

import (
	"net/http"
	"path/filepath"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/modules"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

// Builder composes a runtime from storage backends and optional capabilities.
type Builder struct {
	sqlitePath             string
	sqliteTenantID         string
	sqliteTerminologyScope string
	postgresDSN            string
	tenantID               string

	blobStore      BlobStoreAdapter
	externalSearch ExternalSearchAdapter
	warehouse      WarehouseAdapter

	fhirPathEngine fhirpath.Engine
	sdcService     hahttp.SDCService
	searchEnabled  bool

	syncHubURL            string
	syncHub               hasync.Hub
	syncServer            hasync.HubServer
	syncNodeID            string
	syncMiddleware        func(http.Handler) http.Handler
	httpMiddleware        func(http.Handler) http.Handler
	httpPrincipalResolver hahttp.PrincipalResolver
	httpAuthChecker       hahttp.AuthChecker
	httpRateLimit         hahttp.RateLimitConfig
	moduleAuthorizer      modules.InstallAuthorizer
	moduleVerifier        modules.ModuleVerifier

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

// WithSQLiteTenant sets the tenant namespace used by local sync and
// terminology persistence. It defaults to "local".
func (b *Builder) WithSQLiteTenant(tenantID string) *Builder {
	b.sqliteTenantID = tenantID
	return b
}

// WithSQLiteTerminologyScope sets the terminology namespace used by SQLite.
// It defaults to "default".
func (b *Builder) WithSQLiteTerminologyScope(scope string) *Builder {
	b.sqliteTerminologyScope = scope
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

// WithSyncServer exposes /sync/push and /sync/pull on the HTTP server.
func (b *Builder) WithSyncServer(hub hasync.HubServer) *Builder {
	b.syncServer = hub
	return b
}

// WithSyncMiddleware applies authentication and authorization to the
// runtime's /sync routes. It should bind the authenticated node and tenant to
// the request before the scoped hub is invoked.
func (b *Builder) WithSyncMiddleware(middleware func(http.Handler) http.Handler) *Builder {
	b.syncMiddleware = middleware
	return b
}

// WithHTTPMiddleware wraps the managed FHIR handler with application
// middleware such as authentication, tracing, or request policy.
func (b *Builder) WithHTTPMiddleware(middleware func(http.Handler) http.Handler) *Builder {
	b.httpMiddleware = middleware
	return b
}

// WithHTTPAuth configures the built-in principal extraction and authorization
// middleware for the managed FHIR handler.
func (b *Builder) WithHTTPAuth(resolver hahttp.PrincipalResolver, checker hahttp.AuthChecker) *Builder {
	b.httpPrincipalResolver = resolver
	b.httpAuthChecker = checker
	return b
}

// WithHTTPRateLimit enables process-local request limiting for the managed
// FHIR handler.
func (b *Builder) WithHTTPRateLimit(config hahttp.RateLimitConfig) *Builder {
	b.httpRateLimit = config
	return b
}

// WithModuleAuthorizer configures authorization for module install and upgrade
// operations performed during Build and through the runtime module manager.
func (b *Builder) WithModuleAuthorizer(authorizer modules.InstallAuthorizer) *Builder {
	b.moduleAuthorizer = authorizer
	return b
}

// WithModuleVerifier configures cryptographic or policy verification for
// modules before install and upgrade. Unsigned modules remain supported when
// no verifier is configured.
func (b *Builder) WithModuleVerifier(verifier modules.ModuleVerifier) *Builder {
	b.moduleVerifier = verifier
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
	for _, path := range paths {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
		b.modulePaths = append(b.modulePaths, path)
	}
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
	sqliteTenantID := b.sqliteTenantID
	if sqliteTenantID == "" {
		sqliteTenantID = "local"
	}
	sqliteTerminologyScope := b.sqliteTerminologyScope
	if sqliteTerminologyScope == "" {
		sqliteTerminologyScope = "default"
	}
	return Config{
		Mode:                   mode,
		SQLitePath:             b.sqlitePath,
		SQLiteTenantID:         sqliteTenantID,
		SQLiteTerminologyScope: sqliteTerminologyScope,
		PostgresDSN:            b.postgresDSN,
		TenantID:               b.tenantID,
		SearchEnabled:          b.searchEnabled,
		ModulePaths:            append([]string(nil), b.modulePaths...),
		HTTPAddr:               b.httpAddr,
		SyncEnabled:            syncEnabled,
		SyncHubURL:             b.syncHubURL,
		SyncNodeID:             nodeID,
	}
}
