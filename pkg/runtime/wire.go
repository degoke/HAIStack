package runtime

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/validate"
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
	fns  []func()
	once sync.Once
}

func (c *cleanupStack) add(fn func()) {
	c.fns = append(c.fns, fn)
}

func (c *cleanupStack) run() {
	c.once.Do(func() {
		for i := len(c.fns) - 1; i >= 0; i-- {
			c.fns[i]()
		}
	})
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
	rt.handler = hahttp.WithHealthEndpoints(state.httpHandler, rt.IsStarted)
	rt.jobRunner = state.jobRunner
	rt.syncProcessor = state.syncProcessor
	rt.reindexWorker = state.reindexWorker
	rt.syncEngine = state.syncEngine
	rt.sqliteDB = state.sqliteDB
	rt.postgresDB = state.postgresDB
	// Transfer only the cleanup functions. Copying cleanupStack itself would
	// copy its sync.Once state, which is both unsafe and rejected by vet.
	rt.cleanup.fns = state.cleanup.fns
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

	syncTenantID := b.sqliteTenantID
	if syncTenantID == "" {
		syncTenantID = "local"
	}
	terminologyScope := b.sqliteTerminologyScope
	if terminologyScope == "" {
		terminologyScope = "default"
	}

	return b.wireCommon(ctx, state, persistenceContext{
		definitions:      db.DefinitionStore(),
		installs:         db.RegistryInstallStore(),
		moduleStore:      db.ModuleStore(),
		jobStore:         db.JobStore(),
		resources:        db.ResourceStore(),
		history:          db.HistoryStore(),
		searchStore:      db.SearchStore(),
		sessions:         db,
		outboxEvents:     db.OutboxStore(),
		syncTenantID:     syncTenantID,
		terminologyScope: terminologyScope,
		syncEvents:       db.OutboxStore(),
		syncCursors:      db.CursorStore(),
		syncInbox:        db.InboxStore(),
		syncConflicts:    db.ConflictStore(),
		syncAudit:        db.AuditStore(),
		terminology:      db.TerminologyStore(),
		reindexJobs:      false,
	})
}

func (b *Builder) wirePostgres(ctx context.Context, state *wireState) error {
	opts := make([]postgres.Option, 0, 1)
	if b.postgresSchema != "" {
		opts = append(opts, postgres.WithSchema(b.postgresSchema))
	}
	db, err := postgres.Open(ctx, b.postgresDSN, opts...)
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
		definitions:      db.DefinitionStore(),
		installs:         tdb.RegistryInstallStore(),
		moduleStore:      tdb.ModuleStore(),
		jobStore:         tdb.JobStore(),
		resources:        tdb.ResourceStore(),
		history:          tdb.HistoryStore(),
		searchStore:      tdb.SearchStore(),
		sessions:         tdb,
		outboxEvents:     tdb.EventStore(),
		syncTenantID:     b.tenantID,
		terminologyScope: b.tenantID,
		syncEvents:       tdb.EventStore(),
		syncCursors:      tdb.CursorStore(),
		syncInbox:        tdb.InboxStore(),
		syncConflicts:    tdb.ConflictStore(),
		syncAudit:        tdb.AuditStore(),
		terminology:      tdb.TerminologyStore(),
		reindexJobs:      b.searchEnabled,
	})
}

type persistenceContext struct {
	definitions      store.DefinitionStore
	installs         store.RegistryInstallStore
	moduleStore      store.ModuleStore
	jobStore         store.JobStore
	resources        store.ResourceStore
	history          store.HistoryStore
	searchStore      store.SearchStore
	sessions         store.WriteSessionProvider
	outboxEvents     store.EventStore
	syncTenantID     string
	syncEvents       store.EventStore
	syncCursors      store.CursorStore
	syncInbox        store.InboxStore
	syncConflicts    store.ConflictStore
	syncAudit        store.AuditStore
	reindexJobs      bool
	terminology      store.TerminologyStore
	terminologyScope string
}

func (b *Builder) wireCommon(ctx context.Context, state *wireState, pc persistenceContext) error {
	now := func() time.Time { return time.Now().UTC() }
	termScope := pc.terminologyScope
	if termScope == "" {
		termScope = pc.syncTenantID
	}
	if pc.terminology != nil {
		state.services.TerminologyService = &terminology.LocalService{Store: pc.terminology, ScopeID: termScope}
	}

	var reindexNotifier registry.SearchReindexNotifier
	if pc.reindexJobs {
		reindexNotifier = search.NewReindexNotifier(pc.jobStore)
	}

	regManager := registry.NewManager(registry.Config{
		Definitions:      pc.definitions,
		Installs:         pc.installs,
		Now:              now,
		SearchReindex:    reindexNotifier,
		Terminology:      pc.terminology,
		TerminologyScope: termScope,
		TerminologyCache: state.services.TerminologyService.(terminology.Invalidator),
	})
	if err := regManager.SeedBundled(ctx); err != nil {
		return fmt.Errorf("runtime: seed registry: %w", err)
	}
	// Subscription is part of the REST surface used by the client sub-client;
	// enable its base definition in every runtime so Subscription search does
	// not depend on an unrelated demo/module selection.
	if err := regManager.EnableResource(ctx, "Subscription"); err != nil {
		return fmt.Errorf("runtime: enable Subscription: %w", err)
	}

	modManager := modules.NewManager(modules.Config{
		ModuleStore:          pc.moduleStore,
		DefinitionStore:      pc.definitions,
		RegistryInstallStore: pc.installs,
		RegistryManager:      regManager,
		ResourceStore:        pc.resources,
		Authorizer:           b.moduleAuthorizer,
		Verifier:             b.moduleVerifier,
		Now:                  now,
	})
	state.services.ModuleManager = modManager
	state.services.RegistryManager = regManager

	if err := modManager.InstallAll(ctx, b.modulePaths...); err != nil {
		return fmt.Errorf("runtime: install modules: %w", err)
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

	validateEngine, err := validate.NewEngine(validate.Config{
		InstalledTypes: snapshot,
		ProfileCatalog: validate.NewRegistryProfileCatalog(snapshot),
		FHIRPath:       engine,
	})
	if err != nil {
		return fmt.Errorf("runtime: validate engine: %w", err)
	}
	questionnaireResolver := sdc.StoreQuestionnaireResolver{Resources: pc.resources}
	baseValidator := validate.NewCoreValidator(validateEngine, validate.ValidateOptions{
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		ProfileConstraints:      true,
	})
	validator := &sdc.ResponseValidator{
		Base:     baseValidator,
		Resolver: questionnaireResolver,
		Options: sdc.ValidationOptions{
			Expressions: sdc.FHIRPathExpressions{Engine: engine},
		},
	}

	coreSvc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources:        pc.resources,
		History:          pc.history,
		Sessions:         pc.sessions,
		IDPolicy:         core.DefaultIDPolicy{},
		Validator:        validator,
		Indexer:          indexer,
		Outbox:           &hasync.EventStoreOutbox{Events: pc.outboxEvents},
		Terminology:      pc.terminology,
		TerminologyScope: termScope,
		TerminologyCache: state.services.TerminologyService.(terminology.Invalidator),
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
			Sessions:  pc.sessions,
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
		httpSearchSvc = hahttp.SearchServiceAdapter{
			Svc:                        state.services.SearchService,
			PatientSearchParamResolver: snapshot,
		}
	}

	sdcService := b.sdcService
	if sdcService == nil {
		sdcService = hahttp.CoreSDCService{Resources: state.services.ResourceService, Resolver: sdc.StoreQuestionnaireResolver{Resources: pc.resources}, Provider: sdc.FHIRPathExpressions{Engine: engine}}
	}
	patientRefResolver := &registry.PatientReferenceResolver{
		Snapshot: snapshot,
		Engine:   engine,
	}
	handler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:          hahttp.CoreResourceService{Svc: state.services.ResourceService},
		SearchService:            httpSearchSvc,
		SDCService:               sdcService,
		CapabilitySource:         hahttp.RegistryCapabilitySource{Snapshot: state.services.RegistrySnapshot},
		PatientReferenceResolver: patientRefResolver,
		AuthMiddleware:           b.httpMiddleware,
		PrincipalResolver:        b.httpPrincipalResolver,
		AuthChecker:              b.httpAuthChecker,
		RateLimit:                b.httpRateLimit,
		ServerMetadata: hahttp.ServerMetadata{
			SoftwareName:    "haistack-runtime",
			SoftwareVersion: "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("runtime: http handler: %w", err)
	}
	if hubServer, ok := b.syncHub.(hasync.HubServer); ok {
		state.httpHandler = hahttp.NewRootHandlerWithSyncMiddleware(handler, hubServer, b.syncMiddleware)
	} else if b.syncServer != nil {
		state.httpHandler = hahttp.NewRootHandlerWithSyncMiddleware(handler, b.syncServer, b.syncMiddleware)
	} else {
		state.httpHandler = handler
	}
	return nil
}
