package search_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func openPostgresSearchHarness(t *testing.T) (*core.ResourceService, *search.Service, *registry.Snapshot, *postgres.TenantDB, func()) {
	t.Helper()
	ctx := context.Background()
	db, cleanup := openPostgresTestDB(t)
	tenantID := fmt.Sprintf("search-%d", time.Now().UnixNano())
	if err := db.EnsureTenant(ctx, tenantID); err != nil {
		cleanup()
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := db.Tenant(tenantID)

	manager := registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    tdb.RegistryInstallStore(),
	})
	if err := manager.SeedBundled(ctx); err != nil {
		cleanup()
		t.Fatalf("SeedBundled: %v", err)
	}
	for _, rt := range []string{"Patient", "Observation"} {
		if err := manager.EnableResource(ctx, rt); err != nil {
			cleanup()
			t.Fatalf("EnableResource %s: %v", rt, err)
		}
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		cleanup()
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	reg := search.NewSnapshotRegistry(snapshot)
	eng := testEngine(t)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{Registry: reg, Engine: eng})
	if err != nil {
		cleanup()
		t.Fatalf("NewRegistryIndexer: %v", err)
	}

	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: tdb.ResourceStore(),
		History:   tdb.HistoryStore(),
		Sessions:  tdb,
		IDPolicy:  core.DefaultIDPolicy{},
		Indexer:   indexer,
		Outbox:    &hasync.EventStoreOutbox{Events: tdb.EventStore()},
	})
	if err != nil {
		cleanup()
		t.Fatalf("NewResourceService: %v", err)
	}

	searchSvc, err := search.NewService(search.ServiceConfig{
		Registry:  reg,
		Executor:  search.NewStoreExecutor(tdb.SearchStore(), tdb.ResourceStore()),
		Resources: tdb.ResourceStore(),
		BaseURL:   "Patient",
	})
	if err != nil {
		cleanup()
		t.Fatalf("NewService: %v", err)
	}
	return svc, searchSvc, snapshot, tdb, cleanup
}

func openPostgresTestDB(t *testing.T) (*postgres.DB, func()) {
	t.Helper()
	ctx := context.Background()

	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open TEST_POSTGRES_DSN: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			db.Close()
			t.Fatalf("Migrate: %v", err)
		}
		return db, db.Close
	}

	if !postgresDockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("haistack_search_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}

	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("Migrate: %v", err)
	}

	cleanup := func() {
		db.Close()
		_ = container.Terminate(ctx)
	}
	return db, cleanup
}

func postgresDockerAvailable() bool {
	if os.Getenv("DOCKER_HOST") == "" {
		out, err := exec.Command("docker", "context", "inspect", "-f", "{{.Endpoints.docker.Host}}").Output()
		if err == nil {
			if host := strings.TrimSpace(string(out)); host != "" {
				_ = os.Setenv("DOCKER_HOST", host)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

func TestPostgresSearchByNameAndID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, _, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	for _, p := range []struct {
		id, family, phone string
	}{
		{"pat-1", "Doe", "555-0100"},
		{"pat-2", "Smith", "555-0200"},
	} {
		if _, err := svc.Create(ctx, patientResource(t, p.id, p.family, p.phone)); err != nil {
			t.Fatalf("Create %s: %v", p.id, err)
		}
	}

	result, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search name: %v", err)
	}
	if len(result.Resources) != 1 || result.Resources[0].ID != "pat-1" {
		t.Fatalf("name search = %#v", result.Resources)
	}
	if result.Total == nil || *result.Total != 1 {
		t.Fatalf("total = %#v", result.Total)
	}

	result, err = searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"_id": "pat-2"}))
	if err != nil {
		t.Fatalf("Search _id: %v", err)
	}
	if len(result.Resources) != 1 || result.Resources[0].ID != "pat-2" {
		t.Fatalf("_id search = %#v", result.Resources)
	}
}

func TestPostgresSearchANDOR(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, _, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	for _, p := range []struct {
		id, family string
	}{
		{"pat-1", "Doe"},
		{"pat-2", "Smith"},
		{"pat-3", "Brown"},
	} {
		if _, err := svc.Create(ctx, patientResource(t, p.id, p.family, "555")); err != nil {
			t.Fatalf("Create %s: %v", p.id, err)
		}
	}

	result, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe,Smith"}))
	if err != nil {
		t.Fatalf("Search OR: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("OR search count = %d, want 2", len(result.Resources))
	}

	result, err = searchSvc.Search(ctx, "Patient", mustValues(t, map[string][]string{
		"name": {"Doe", "Smith"},
	}))
	if err != nil {
		t.Fatalf("Search AND: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Fatalf("AND search should match none, got %d", len(result.Resources))
	}
}

func TestPostgresSearchPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, _, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("pat-%d", i)
		if _, err := svc.Create(ctx, patientResource(t, id, "Family", "555")); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	page1, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"_count": "2", "_offset": "0"}))
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"_count": "2", "_offset": "2"}))
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page1.Resources) != 2 || len(page2.Resources) != 2 {
		t.Fatalf("page sizes = %d and %d", len(page1.Resources), len(page2.Resources))
	}
	if page1.Resources[0].ID == page2.Resources[0].ID {
		t.Fatalf("pages overlap: %s and %s", page1.Resources[0].ID, page2.Resources[0].ID)
	}
	if page1.Links["next"] == "" {
		t.Fatal("expected next link")
	}
}

func TestPostgresReindexRestoresMissingRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, snapshot, tdb, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.Create(ctx, patientResource(t, "pat-1", "Doe", "555")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := tdb.SearchStore().RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}

	result, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search before reindex: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Fatal("expected no matches before reindex")
	}

	reg := search.NewSnapshotRegistry(snapshot)
	worker := &search.ReindexWorker{
		Registry:  reg,
		Indexer:   mustIndexer(t, reg),
		Resources: tdb.ResourceStore(),
		Search:    tdb.SearchStore(),
	}
	if err := worker.ReindexAll(ctx, "Patient"); err != nil {
		t.Fatalf("ReindexAll: %v", err)
	}

	result, err = searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search after reindex: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected one match after reindex, got %d", len(result.Resources))
	}
}

func TestPostgresDeleteRemovesSearchRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, tdb, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.Create(ctx, patientResource(t, "pat-1", "Doe", "555")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ids, err := tdb.SearchStore().LookupMatch(ctx, store.SearchMatch{
		ResourceType: "Patient",
		FieldKey:     "string.name",
		Value:        "Doe",
	})
	if err != nil {
		t.Fatalf("LookupMatch: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no search rows after delete, got %v", ids)
	}
	_, err = searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestPostgresSearchSortByLastUpdated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, _, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.Create(ctx, patientResource(t, "pat-1", "Alpha", "555-0001")); err != nil {
		t.Fatalf("Create pat-1: %v", err)
	}
	if _, err := svc.Create(ctx, patientResource(t, "pat-2", "Beta", "555-0002")); err != nil {
		t.Fatalf("Create pat-2: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := svc.Update(ctx, patientResource(t, "pat-2", "Beta", "555-0002")); err != nil {
		t.Fatalf("Update pat-2: %v", err)
	}

	result, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"_sort": "-_lastUpdated"}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("result count = %d, want 2", len(result.Resources))
	}
	if result.Resources[0].ID != "pat-2" {
		t.Fatalf("first result = %q, want pat-2", result.Resources[0].ID)
	}
}

func TestPostgresUpdateRemovesStaleIndexRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, _, _, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.Create(ctx, patientResource(t, "pat-1", "Doe", "555")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(ctx, patientResource(t, "pat-1", "Smith", "555")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	oldResult, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search Doe: %v", err)
	}
	if len(oldResult.Resources) != 0 {
		t.Fatalf("expected no Doe matches after update, got %d", len(oldResult.Resources))
	}

	newResult, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Smith"}))
	if err != nil {
		t.Fatalf("Search Smith: %v", err)
	}
	if len(newResult.Resources) != 1 || newResult.Resources[0].ID != "pat-1" {
		t.Fatalf("Smith search = %#v", newResult.Resources)
	}
}

func TestPostgresReindexJobRunner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	svc, searchSvc, snapshot, tdb, cleanup := openPostgresSearchHarness(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.Create(ctx, patientResource(t, "pat-1", "Doe", "555")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := tdb.SearchStore().RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}

	reg := search.NewSnapshotRegistry(snapshot)
	worker := &search.ReindexWorker{
		Registry:  reg,
		Indexer:   mustIndexer(t, reg),
		Resources: tdb.ResourceStore(),
		Search:    tdb.SearchStore(),
	}
	runner := &search.ReindexJobRunner{Jobs: tdb.JobStore(), Worker: worker}
	if _, err := search.EnqueueReindex(ctx, tdb.JobStore(), "Patient"); err != nil {
		t.Fatalf("EnqueueReindex: %v", err)
	}
	processed, err := runner.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("expected job to be processed")
	}

	result, err := searchSvc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected one match after job reindex, got %d", len(result.Resources))
	}
}

func TestPostgresEnableResourceSchedulesReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	ctx := context.Background()
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	tenantID := fmt.Sprintf("search-reindex-%d", time.Now().UnixNano())
	if err := db.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := db.Tenant(tenantID)
	manager := registry.NewManager(registry.Config{
		Definitions:   db.DefinitionStore(),
		Installs:      tdb.RegistryInstallStore(),
		SearchReindex: search.NewReindexNotifier(tdb.JobStore()),
	})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Observation"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}

	claimed, err := tdb.JobStore().ClaimNext(ctx, search.ReindexJobType)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected reindex job after EnableResource")
	}
	var payload search.ReindexPayload
	if err := json.Unmarshal(claimed.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload.ResourceType != "Observation" {
		t.Fatalf("payload resourceType = %q", payload.ResourceType)
	}
}

func mustIndexer(t *testing.T, reg *search.SnapshotRegistry) search.Indexer {
	t.Helper()
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}
	return indexer
}

func mustValues(t *testing.T, params any) url.Values {
	t.Helper()
	values := url.Values{}
	switch p := params.(type) {
	case map[string]string:
		for k, v := range p {
			values.Set(k, v)
		}
	case map[string][]string:
		for k, vs := range p {
			for _, v := range vs {
				values.Add(k, v)
			}
		}
	default:
		t.Fatalf("unsupported params type %T", params)
	}
	return values
}
