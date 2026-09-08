package host

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ReferenceStack is a SQLite-backed FHIR stack for the inferno-reference host.
type ReferenceStack struct {
	DB      *sqlite.DB
	Handler *Handler
	close   func() error
}

// Close releases stack resources.
func (s *ReferenceStack) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// OpenReferenceStack builds a SMART reference host backed by SQLite.
// publicBase is the external origin (for example http://localhost:8765).
func OpenReferenceStack(ctx context.Context, dbPath, publicBase string) (*ReferenceStack, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("smart/host: db path required")
	}
	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, err
	}
	closeFn := func() error { return db.Close() }

	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	manager := registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    db.RegistryInstallStore(),
		Now:         func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) },
	})
	if err := manager.SeedBundled(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		_ = db.Close()
		return nil, err
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	resourceStore := db.ResourceStore()
	searchStore := db.SearchStore()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: resourceStore,
		History:   db.HistoryStore(),
		Sessions:  db,
		Indexer:   &noopIndexer{},
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	searchSvc, err := search.NewService(search.ServiceConfig{
		Registry:  search.NewSnapshotRegistry(snapshot),
		Executor:  search.NewStoreExecutor(searchStore, resourceStore),
		Resources: resourceStore,
		BaseURL:   "/fhir/Patient",
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	patientRef := &registry.PatientReferenceResolver{Snapshot: snapshot, Engine: engine}
	fhirHandler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService: hahttp.CoreResourceService{Svc: svc},
		SearchService: hahttp.SearchServiceAdapter{
			Svc:                        searchSvc,
			PatientSearchParamResolver: snapshot,
		},
		PatientReferenceResolver: patientRef,
		CapabilitySource:         hahttp.RegistryCapabilitySource{Snapshot: snapshot},
		ServerMetadata: hahttp.ServerMetadata{
			SoftwareName:    "haistack-inferno-reference",
			SoftwareVersion: "0.1.0",
		},
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	publicBase = strings.TrimRight(publicBase, "/")
	smartHandler, err := NewHandler(fhirHandler, Config{
		FHIRBasePath:  publicBase + "/fhir",
		OAuthBasePath: publicBase + "/oauth",
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &ReferenceStack{DB: db, Handler: smartHandler, close: closeFn}, nil
}

// DefaultDBPath returns a sqlite path under dir.
func DefaultDBPath(dir string) string {
	return filepath.Join(dir, "inferno.db")
}

type noopIndexer struct{}

func (noopIndexer) Build(_ context.Context, _ *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	return nil, nil
}
