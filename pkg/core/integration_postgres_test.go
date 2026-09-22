package core_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/core"
	"github.com/degoke/haistack/pkg/postgres"
	hasync "github.com/degoke/haistack/pkg/sync"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func openPostgresHarness(t *testing.T) (*core.ResourceService, func()) {
	t.Helper()
	ctx := context.Background()

	db, cleanup := openPostgresTestDB(t)
	tenantID := fmt.Sprintf("core-%d", time.Now().UnixNano())
	if err := db.EnsureTenant(ctx, tenantID); err != nil {
		cleanup()
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := db.Tenant(tenantID)

	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: tdb.ResourceStore(),
		History:   tdb.HistoryStore(),
		Sessions:  tdb,
		IDPolicy:  core.DefaultIDPolicy{},
		Indexer:   &familyIndexer{},
		Outbox:    &hasync.EventStoreOutbox{Events: tdb.EventStore()},
	})
	if err != nil {
		cleanup()
		t.Fatalf("NewResourceService: %v", err)
	}
	return svc, cleanup
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
		tcpostgres.WithDatabase("haistack_core_test"),
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

func TestPostgresCoreCreateReadUpdateDelete(t *testing.T) {
	svc, cleanup := openPostgresHarness(t)
	defer cleanup()
	ctx := context.Background()

	created, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.VersionID == "" {
		t.Fatal("missing versionId")
	}

	read, err := svc.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != "pat-1" {
		t.Fatalf("Read ID = %q", read.ID)
	}

	updated, err := svc.Update(ctx, patientEnvelope("pat-1", "Smith"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VersionID == created.VersionID {
		t.Fatal("expected new version on update")
	}

	if err := svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = svc.Read(ctx, "Patient", "pat-1")
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}
