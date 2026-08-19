package appkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// SQLiteStack is a ready-to-use local stack for examples.
type SQLiteStack struct {
	DB              *sqlite.DB
	RegistryManager *registry.Manager
	Snapshot        *registry.Snapshot
	SearchRegistry  *search.SnapshotRegistry
	FHIRPath        fhirpath.Engine
	Indexer         *search.RegistryIndexer
	ResourceService *core.ResourceService
	SearchService   *search.Service
}

// NewSQLiteStack composes a local SQLite-backed resource and search stack.
func NewSQLiteStack(ctx context.Context, dbPath string, resourceTypes ...string) (*SQLiteStack, error) {
	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}

	manager := registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    db.RegistryInstallStore(),
	})
	if err := manager.SeedBundled(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("seed registry: %w", err)
	}
	if err := manager.EnableResource(ctx, "Subscription"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable Subscription: %w", err)
	}

	if len(resourceTypes) == 0 {
		resourceTypes = []string{"Patient"}
	}
	seen := make(map[string]struct{}, len(resourceTypes))
	for _, rt := range resourceTypes {
		if _, ok := seen[rt]; ok {
			continue
		}
		seen[rt] = struct{}{}
		if err := manager.EnableResource(ctx, rt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable resource %s: %w", rt, err)
		}
	}

	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("rebuild registry snapshot: %w", err)
	}

	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("new fhirpath engine: %w", err)
	}

	searchRegistry := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: searchRegistry,
		Engine:   engine,
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("new search indexer: %w", err)
	}

	resourceService, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: db.ResourceStore(),
		History:   db.HistoryStore(),
		Sessions:  db,
		Indexer:   indexer,
		Outbox:    &hasync.EventStoreOutbox{Events: db.OutboxStore()},
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("new resource service: %w", err)
	}

	searchService, err := search.NewService(search.ServiceConfig{
		Registry:  searchRegistry,
		Executor:  search.NewStoreExecutor(db.SearchStore(), db.ResourceStore()),
		Resources: db.ResourceStore(),
		BaseURL:   "/fhir",
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("new search service: %w", err)
	}

	return &SQLiteStack{
		DB:              db,
		RegistryManager: manager,
		Snapshot:        snapshot,
		SearchRegistry:  searchRegistry,
		FHIRPath:        engine,
		Indexer:         indexer,
		ResourceService: resourceService,
		SearchService:   searchService,
	}, nil
}

func (s *SQLiteStack) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// SyncIndexerBridge adapts a search.Indexer for sync pull-apply indexing.
type SyncIndexerBridge struct {
	Indexer search.Indexer
}

func (b SyncIndexerBridge) BuildSearchEntries(ctx context.Context, res *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	if b.Indexer == nil {
		return nil, nil
	}
	return b.Indexer.Build(ctx, res)
}

// EnvelopeFromJSON parses FHIR JSON into a ResourceEnvelope.
func EnvelopeFromJSON(resourceType string, raw []byte) (*types.ResourceEnvelope, error) {
	return types.NewJSONCodec().ParseJSON(resourceType, raw)
}

// PrettyJSON returns indented JSON for display.
func PrettyJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

// RepoPath resolves a repository-relative path regardless of the current working directory.
func RepoPath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join(parts...)
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	all := append([]string{root}, parts...)
	return filepath.Join(all...)
}
