package command_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/runtime"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
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
		tcpostgres.WithDatabase("haistack_cli_test"),
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

func TestServeBuildsAndStartsSQLite(t *testing.T) {
	dir := t.TempDir()
	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "serve.db"), coreModule)

	cfg, err := config.Load(filepath.Join(dir, config.DefaultConfigFile), config.Overrides{})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	rt, err := app.BuildRuntime(ctx, cfg, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()
	if rt.HTTPAddr() == nil {
		t.Fatal("expected bound HTTP address")
	}
	resp, err := http.Get("http://" + rt.HTTPAddr().String() + "/fhir/metadata")
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d", resp.StatusCode)
	}
}

func TestModuleInstallAndReindexSQLite(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "reindex.db"), coreModule)

	patientPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(patientPath, []byte(`{"resourceType":"Patient","name":[{"family":"Reindex"}]}`), 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}
	if _, _, err := runCLI(t, dir, "import", patientPath); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, _, err := runCLI(t, dir, "module", "install", coreModule); err != nil {
		t.Fatalf("module install: %v", err)
	}
	if _, _, err := runCLI(t, dir, "reindex", "Patient"); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}

func TestSyncPushPullStatusSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sync integration in short mode")
	}
	ctx := context.Background()
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	hubTenant := fmt.Sprintf("cli-hub-%d", time.Now().UnixNano())
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
	defer hubDB.Close()

	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "sync.db"), coreModule)

	cfg, err := config.Load(filepath.Join(dir, config.DefaultConfigFile), config.Overrides{})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	rt, err := runtime.New().
		WithSQLite(cfg.Storage.SQLitePath).
		WithSearch().
		WithModules(cfg.Runtime.ModulePaths...).
		WithSyncHub(hub).
		WithSyncNode("cli-device").
		Build(ctx)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	patient := []byte(`{"resourceType":"Patient","name":[{"family":"Sync"}]}`)
	env, err := types.NewJSONCodec().ParseJSON("Patient", patient)
	if err != nil {
		t.Fatalf("parse patient: %v", err)
	}
	if _, err := rt.Services().ResourceService.Create(ctx, env); err != nil {
		t.Fatalf("create patient: %v", err)
	}
	if _, err := rt.SyncEngine().Push(ctx); err != nil {
		t.Fatalf("push: %v", err)
	}
	if _, err := rt.SyncEngine().Pull(ctx); err != nil {
		t.Fatalf("pull: %v", err)
	}

	if _, _, err := runCLI(t, dir, "sync", "status"); err != nil {
		t.Fatalf("sync status: %v", err)
	}
}

func TestPostgresCLIHonorsTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres CLI integration in short mode")
	}
	ctx := context.Background()
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()
	tenantID := fmt.Sprintf("cli-%d", time.Now().UnixNano())

	dir := t.TempDir()
	coreModule := repoCoreModule(t)
	content := fmt.Sprintf(`storage:
  driver: postgres
  postgresDSN: %q
  tenantID: %q
runtime:
  enableSearch: true
  modulePaths:
    - %s
sync:
  nodeID: pg-node
`, dsn, tenantID, coreModule)
	if err := os.WriteFile(filepath.Join(dir, config.DefaultConfigFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(filepath.Join(dir, config.DefaultConfigFile), config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt, err := app.BuildRuntime(ctx, cfg, "")
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()
	if rt.Services().TenantDB == nil || rt.Services().TenantDB.TenantID() != tenantID {
		t.Fatalf("tenant db = %v", rt.Services().TenantDB)
	}

	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if _, _, err := runCLI(t, dir, "sync", "status", "--output", "json"); err != nil {
		t.Fatalf("sync status: %v", err)
	}
}

func TestPostgresSearchAndReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres search integration in short mode")
	}
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()
	tenantID := fmt.Sprintf("cli-search-%d", time.Now().UnixNano())

	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	content := fmt.Sprintf(`storage:
  driver: postgres
  postgresDSN: %q
  tenantID: %q
runtime:
  enableSearch: true
  modulePaths:
    - %s
`, dsn, tenantID, coreModule)
	if err := os.WriteFile(filepath.Join(dir, config.DefaultConfigFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	patientPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(patientPath, []byte(`{"resourceType":"Patient","name":[{"family":"PgSearch"}]}`), 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}
	if _, _, err := runCLI(t, dir, "import", patientPath); err != nil {
		t.Fatalf("import: %v", err)
	}
	stdout, _, err := runCLI(t, dir, "search", "Patient", "name=PgSearch")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(stdout, "Patient/") {
		t.Fatalf("search stdout = %q", stdout)
	}
	if _, _, err := runCLI(t, dir, "reindex", "Patient"); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}
