package runtime

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

type wireState struct {
	services      *ServiceContainer
	httpHandler   http.Handler
	jobRunner     *jobs.Runner
	syncProcessor *hasync.JobProcessor
	reindexWorker *search.ReindexWorker
	syncEngine    *hasync.Engine
	sqliteDB      *sqlite.DB
	postgresDB    *postgres.DB
	cleanup       cleanupStack
}

type cleanupStack struct {
	fns []func()
}

func (c *cleanupStack) add(fn func()) {
	c.fns = append(c.fns, fn)
}

func (c *cleanupStack) run() {
	for i := len(c.fns) - 1; i >= 0; i-- {
		c.fns[i]()
	}
}

func (b *Builder) wire(ctx context.Context, rt *Runtime) error {
	mode, err := b.resolveMode()
	if err != nil {
		return err
	}
	rt.mode = mode
	rt.config = b.normalizedConfig(mode)

	if rt.config.SyncEnabled && b.syncHub == nil && b.syncHubURL == "" {
		return ErrSyncHubRequired
	}

	state := &wireState{services: &ServiceContainer{
		BlobStore:      b.blobStore,
		ExternalSearch: b.externalSearch,
		Warehouse:      b.warehouse,
	}}

	switch mode {
	case ModeLocalSQLite:
		err = b.wireSQLite(ctx, state)
	case ModeEdgePostgresAllInOne, ModeCloudPostgresPlusExternalServices:
		err = b.wirePostgres(ctx, state)
	default:
		err = fmt.Errorf("%w: %s", ErrInvalidModeCombination, mode)
	}
	if err != nil {
		state.cleanup.run()
		return err
	}

	rt.services = state.services
	rt.handler = state.httpHandler
	rt.jobRunner = state.jobRunner
	rt.syncProcessor = state.syncProcessor
	rt.reindexWorker = state.reindexWorker
	rt.syncEngine = state.syncEngine
	rt.sqliteDB = state.sqliteDB
	rt.postgresDB = state.postgresDB
	rt.cleanup = state.cleanup
	return nil
}

func (b *Builder) wireSQLite(ctx context.Context, state *wireState) error {
	db, err := sqlite.Open(b.sqlitePath)
	if err != nil {
		return fmt.Errorf("runtime: open sqlite: %w", err)
	}
	state.sqliteDB = db
	state.cleanup.add(func() { _ = db.Close() })

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("runtime: migrate sqlite: %w", err)
	}

	return b.wireCommon(ctx, state, persistenceContext{
		definitions:   db.DefinitionStore(),
		installs:      db.RegistryInstallStore(),
		moduleStore:   db.ModuleStore(),
		jobStore:      db.JobStore(),
		resources:     db.ResourceStore(),
		history:       db.HistoryStore(),
		searchStore:   db.SearchStore(),
		sessions:      db,
		outboxEvents:  db.OutboxStore(),
		syncTenantID:  "local",
		syncEvents:    db.OutboxStore(),
		syncCursors:   db.CursorStore(),
		syncInbox:     db.InboxStore(),
		syncConflicts: db.ConflictStore(),
		syncAudit:     db.AuditStore(),
		reindexJobs:   false,
	})
}

func (b *Builder) wirePostgres(ctx context.Context, state *wireState) error {
	db, err := postgres.Open(ctx, b.postgresDSN)
	if err != nil {
		return fmt.Errorf("runtime: open postgres: %w", err)
	}
	state.postgresDB = db
	state.cleanup.add(func() { db.Close() })

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("runtime: migrate postgres: %w", err)
	}
	if err := db.EnsureTenant(ctx, b.tenantID); err != nil {
		return fmt.Errorf("runtime: ensure tenant: %w", err)
	}
	tdb := db.Tenant(b.tenantID)
	state.services.TenantDB = tdb

	return b.wireCommon(ctx, state, persistenceContext{
		definitions:   db.DefinitionStore(),
		installs:      tdb.RegistryInstallStore(),
		moduleStore:   tdb.ModuleStore(),
		jobStore:      tdb.JobStore(),
		resources:     tdb.ResourceStore(),
		history:       tdb.HistoryStore(),
		searchStore:   tdb.SearchStore(),
		sessions:      tdb,
		outboxEvents:  tdb.EventStore(),
		syncTenantID:  b.tenantID,
		syncEvents:    tdb.EventStore(),
		syncCursors:   tdb.CursorStore(),
		syncInbox:     tdb.InboxStore(),
		syncConflicts: tdb.ConflictStore(),
		syncAudit:     tdb.AuditStore(),
		reindexJobs:   b.searchEnabled,
	})
}

type persistenceContext struct {
	definitions   store.DefinitionStore
	installs      store.RegistryInstallStore
	moduleStore   store.ModuleStore
	jobStore      store.JobStore
	resources     store.ResourceStore
	history       store.HistoryStore
	searchStore   store.SearchStore
	sessions      store.WriteSessionProvider
	outboxEvents  store.EventStore
	syncTenantID  string
	syncEvents    store.EventStore
	syncCursors   store.CursorStore
	syncInbox     store.InboxStore
	syncConflicts store.ConflictStore
	syncAudit     store.AuditStore
	reindexJobs   bool
}

func (b *Builder) wireCommon(ctx context.Context, state *wireState, pc persistenceContext) error {
	now := func() time.Time { return time.Now().UTC() }

	var reindexNotifier registry.SearchReindexNotifier
	if pc.reindexJobs {
		reindexNotifier = search.NewReindexNotifier(pc.jobStore)
	}

	regManager := registry.NewManager(registry.Config{
		Definitions:   pc.definitions,
		Installs:      pc.installs,
		Now:           now,
		SearchReindex: reindexNotifier,
	})
	if err := regManager.SeedBundled(ctx); err != nil {
		return fmt.Errorf("runtime: seed registry: %w", err)
	}

	modManager := modules.NewManager(modules.Config{
		ModuleStore:          pc.moduleStore,
		DefinitionStore:      pc.definitions,
		RegistryInstallStore: pc.installs,
		RegistryManager:      regManager,
		Now:                  now,
	})
	state.services.ModuleManager = modManager
	state.services.RegistryManager = regManager

	for _, path := range b.modulePaths {
		if _, err := modManager.Install(ctx, path); err != nil {
			return fmt.Errorf("runtime: install module %q: %w", path, err)
		}
	}

	snapshot, err := regManager.RebuildSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("runtime: rebuild registry snapshot: %w", err)
	}
	state.services.RegistrySnapshot = snapshot

	engine := b.fhirPathEngine
	if engine == nil {
		engine, err = fhirpath.NewEngine(fhirpath.Config{})
		if err != nil {
			return fmt.Errorf("runtime: fhirpath engine: %w", err)
		}
	}
	state.services.FHIRPathEngine = engine

	var indexer search.Indexer
	if b.searchEnabled {
		reg := search.NewSnapshotRegistry(snapshot)
		indexer, err = search.NewRegistryIndexer(search.RegistryIndexerConfig{
			Registry: reg,
			Engine:   engine,
		})
		if err != nil {
			return fmt.Errorf("runtime: registry indexer: %w", err)
		}
	}

	validateEngine, err := validate.NewEngine(validate.Config{InstalledTypes: snapshot})
	if err != nil {
		return fmt.Errorf("runtime: validate engine: %w", err)
	}
	validator := validate.NewCoreValidator(validateEngine, validate.ValidateOptions{})

	coreSvc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: pc.resources,
		History:   pc.history,
		Sessions:  pc.sessions,
		IDPolicy:  core.DefaultIDPolicy{},
		Validator: validator,
		Indexer:   indexer,
		Outbox:    &hasync.EventStoreOutbox{Events: pc.outboxEvents},
	})
	if err != nil {
		return fmt.Errorf("runtime: resource service: %w", err)
	}
	state.services.ResourceService = coreSvc

	if b.searchEnabled {
		reg := search.NewSnapshotRegistry(snapshot)
		executorBackend, ok := pc.searchStore.(store.SearchQueryExecutor)
		if !ok {
			return fmt.Errorf("runtime: search store does not implement query execution")
		}
		if b.externalSearch != nil && b.externalSearch.SearchExecutor() != nil {
			executorBackend = b.externalSearch.SearchExecutor()
		}
		executor := search.NewStoreExecutor(executorBackend, pc.resources)
		searchSvc, err := search.NewService(search.ServiceConfig{
			Registry:  reg,
			Executor:  executor,
			Resources: pc.resources,
			BaseURL:   "/fhir",
		})
		if err != nil {
			return fmt.Errorf("runtime: search service: %w", err)
		}
		state.services.SearchService = searchSvc
	}

	if b.syncHub != nil || b.syncHubURL != "" {
		cfg := b.normalizedConfig(ModeLocalSQLite)
		hub := b.syncHub
		if hub == nil {
			hub, err = newHTTPSyncHub(b.syncHubURL, cfg.SyncNodeID, pc.syncTenantID)
			if err != nil {
				return err
			}
		}

		syncCfg := hasync.Config{
			NodeID:    cfg.SyncNodeID,
			TenantID:  pc.syncTenantID,
			Events:    pc.syncEvents,
			Cursors:   pc.syncCursors,
			Inbox:     pc.syncInbox,
			Resources: pc.resources,
			History:   pc.history,
			Conflicts: pc.syncConflicts,
			Jobs:      pc.jobStore,
			Audit:     pc.syncAudit,
			Search:    pc.searchStore,
			Hub:       hub,
		}
		if indexer != nil {
			syncCfg.SearchIndexer = &indexerSyncBridge{indexer: indexer}
		}
		engine := hasync.NewEngine(syncCfg)
		state.services.SyncEngine = engine
		state.syncEngine = engine
		state.syncProcessor = &hasync.JobProcessor{Engine: engine, Jobs: pc.jobStore}
	}

	if pc.reindexJobs && indexer != nil {
		state.reindexWorker = &search.ReindexWorker{
			Registry:  search.NewSnapshotRegistry(snapshot),
			Indexer:   indexer,
			Resources: pc.resources,
			Search:    pc.searchStore,
		}
		runner := jobs.NewRunner(pc.jobStore)
		if err := runner.Register(search.ReindexJobType, jobs.HandlerFunc(state.reindexWorker.HandleJob)); err != nil {
			return fmt.Errorf("runtime: register reindex handler: %w", err)
		}
		state.jobRunner = runner
	}

	var httpSearchSvc hahttp.SearchService
	if state.services.SearchService != nil {
		httpSearchSvc = hahttp.SearchServiceAdapter{Svc: state.services.SearchService}
	}

	handler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:  hahttp.CoreResourceService{Svc: state.services.ResourceService},
		SearchService:    httpSearchSvc,
		CapabilitySource: hahttp.RegistryCapabilitySource{Snapshot: state.services.RegistrySnapshot},
		ServerMetadata: hahttp.ServerMetadata{
			SoftwareName:    "haistack-runtime",
			SoftwareVersion: "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("runtime: http handler: %w", err)
	}
	state.httpHandler = handler
	return nil
}
