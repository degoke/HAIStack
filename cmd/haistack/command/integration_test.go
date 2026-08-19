package command_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/runtime"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit/postgrestest"
	"github.com/degoke/health-ai-stack/pkg/types"
)

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
	for _, path := range []string{"/healthz", "/readyz"} {
		probe, err := http.Get("http://" + rt.HTTPAddr().String() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = probe.Body.Close()
		if probe.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, probe.StatusCode)
		}
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
	if stdout, _, err := runCLI(t, dir, "module", "list", "--output", "json"); err != nil || !strings.Contains(stdout, `"name": "core"`) {
		t.Fatalf("module list: output=%q err=%v", stdout, err)
	}
	if stdout, _, err := runCLI(t, dir, "module", "inspect", "core", "--output", "json"); err != nil || !strings.Contains(stdout, `"version": "1.0.0"`) {
		t.Fatalf("module inspect: output=%q err=%v", stdout, err)
	}
	if stdout, _, err := runCLI(t, dir, "module", "plan", coreModule, "--output", "json"); err != nil || !strings.Contains(stdout, `"action": "noop"`) {
		t.Fatalf("module plan: output=%q err=%v", stdout, err)
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
	dsn := postgrestest.SharedDSN(t)

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
		WithSQLiteTenant(hubTenant).
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
	dsn := postgrestest.SharedDSN(t)
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
	dsn := postgrestest.SharedDSN(t)
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

	ctx := context.Background()
	cfg, err := config.Load(filepath.Join(dir, config.DefaultConfigFile), config.Overrides{})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	session, err := app.OpenSession(ctx, cfg)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	patient, err := app.ReadResourceFile(patientPath)
	if err != nil {
		_ = session.Close(ctx)
		t.Fatalf("read patient: %v", err)
	}
	if _, _, err := app.UpsertResource(ctx, session.Runtime.Services().ResourceService, patient); err != nil {
		_ = session.Close(ctx)
		t.Fatalf("upsert patient: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close session: %v", err)
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
