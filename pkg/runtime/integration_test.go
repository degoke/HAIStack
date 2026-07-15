package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/runtime"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func openPostgresDSN(t *testing.T) (string, func()) {
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
		return dsn, db.Close
	}

	if !dockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("haistack_runtime_test"),
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
	db.Close()

	return dsn, func() { _ = container.Terminate(ctx) }
}

func dockerAvailable() bool {
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

func TestSQLiteIntegrationBuildStartHTTPShutdown(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime.db")
	coreModule := filepath.Join("..", "..", "modules", "core")

	rt, err := runtime.New().
		WithSQLite(dbPath).
		WithSearch().
		WithModules(coreModule).
		WithHTTP("127.0.0.1:0").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if rt.Mode() != runtime.ModeLocalSQLite {
		t.Fatalf("mode = %q", rt.Mode())
	}
	if rt.Services().RegistrySnapshot == nil {
		t.Fatal("expected registry snapshot")
	}
	if !rt.Services().RegistrySnapshot.IsResourceEnabled("Patient") {
		t.Fatal("expected Patient enabled from core module")
	}
	if rt.Handler() == nil {
		t.Fatal("expected HTTP handler")
	}
	if rt.ReindexWorker() != nil {
		t.Fatal("sqlite mode should not start reindex worker")
	}

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt.HTTPAddr() == nil {
		t.Fatal("expected bound HTTP address")
	}

	url := "http://" + rt.HTTPAddr().String() + "/fhir/metadata"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("metadata status = %d body = %s", resp.StatusCode, body)
	}

	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestSQLiteSyncWorkerStartsWhenConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sync integration in short mode")
	}
	ctx := context.Background()
	dsn, pgCleanup := openPostgresDSN(t)
	defer pgCleanup()

	hubTenant := fmt.Sprintf("hub-%d", time.Now().UnixNano())
	hubDB, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open hub db: %v", err)
	}
	if err := hubDB.Migrate(ctx); err != nil {
		hubDB.Close()
		t.Fatalf("migrate hub: %v", err)
	}
	if err := hubDB.EnsureTenant(ctx, hubTenant); err != nil {
		hubDB.Close()
		t.Fatalf("ensure hub tenant: %v", err)
	}
	hub := &hasync.PostgresHub{Tenant: hubDB.Tenant(hubTenant)}
	t.Cleanup(func() { hubDB.Close() })

	rt, err := runtime.New().
		WithSQLite(filepath.Join(t.TempDir(), "device.db")).
		WithSyncHub(hub).
		WithSyncNode("device-test").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rt.SyncEngine() == nil {
		t.Fatal("expected sync engine")
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestPostgresEdgeIntegrationBuildStartHTTPShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration in short mode")
	}
	ctx := context.Background()
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	tenantID := fmt.Sprintf("edge-%d", time.Now().UnixNano())
	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, tenantID).
		WithSearch().
		WithHTTP("127.0.0.1:0").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	if rt.Mode() != runtime.ModeEdgePostgresAllInOne {
		t.Fatalf("mode = %q", rt.Mode())
	}
	if rt.Services().TenantDB == nil {
		t.Fatal("expected tenant DB")
	}
	if rt.ReindexWorker() == nil {
		t.Fatal("expected reindex worker for postgres search")
	}

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp, err := http.Get("http://" + rt.HTTPAddr().String() + "/fhir/metadata")
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d", resp.StatusCode)
	}

	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestCloudSeamAdaptersExposed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres cloud seam test in short mode")
	}
	ctx := context.Background()
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	blob := runtime.TestNoopBlobStore()
	searchAdapter := runtime.TestNoopExternalSearch()
	warehouse := runtime.TestNoopWarehouse()

	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, fmt.Sprintf("cloud-%d", time.Now().UnixNano())).
		WithExternalBlobStore(blob).
		WithExternalSearch(searchAdapter).
		WithExternalWarehouse(warehouse).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	svc := rt.Services()
	if svc.BlobStore != blob {
		t.Fatal("blob adapter not exposed")
	}
	if svc.ExternalSearch != searchAdapter {
		t.Fatal("search adapter not exposed")
	}
	if svc.Warehouse != warehouse {
		t.Fatal("warehouse adapter not exposed")
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start with cloud adapters: %v", err)
	}
}

func TestStartupFailureRollsBack(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(filepath.Join(t.TempDir(), "rollback.db")).
		WithHTTP("\x00:invalid").
		Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err == nil {
		_ = rt.Shutdown(context.Background())
		t.Fatal("expected Start to fail on invalid listen address")
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after failed Start: %v", err)
	}
}

func TestBackgroundWorkersStopOnShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping worker shutdown test in short mode")
	}
	ctx := context.Background()
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, fmt.Sprintf("workers-%d", time.Now().UnixNano())).
		WithSearch().
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestMetadataJSONShape(t *testing.T) {
	ctx := context.Background()
	rt, err := runtime.New().
		WithSQLite(filepath.Join(t.TempDir(), "meta.db")).
		WithHTTP("127.0.0.1:0").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	resp, err := http.Get("http://" + rt.HTTPAddr().String() + "/fhir/metadata")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var metadata map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if metadata["resourceType"] != "CapabilityStatement" {
		t.Fatalf("resourceType = %v", metadata["resourceType"])
	}
}

func TestSearchDisabledReturnsNotSupported(t *testing.T) {
	ctx := context.Background()
	rt, err := runtime.New().
		WithSQLite(filepath.Join(t.TempDir(), "no-search.db")).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient?name=smith", nil)
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not-supported") {
		t.Fatalf("expected not-supported outcome, body = %s", rec.Body.String())
	}
}
