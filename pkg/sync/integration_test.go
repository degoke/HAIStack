package sync_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSQLiteDevicePostgresHubIntegration(t *testing.T) {
	ctx := context.Background()
	pdb, cleanup := openIntegrationPostgres(t)
	defer cleanup()

	tenantID := fmt.Sprintf("tenant-sync-%d", time.Now().UnixNano())
	if err := pdb.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := pdb.Tenant(tenantID)
	hub := &hasync.PostgresHub{Tenant: tdb}

	device, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })
	if err := device.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	now := time.Date(2026, 7, 6, 16, 0, 0, 0, time.UTC)
	res := sampleResource("p-sync", "local-v1")
	if _, err := device.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: res,
		Action:   store.VersionActionCreate,
		Version: store.ResourceVersion{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     res,
			Hash:         res.Hash,
		},
		Event: store.ResourceEvent{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.EventActionCreate,
			Timestamp:    now,
			Hash:         res.Hash,
		},
	}); err != nil {
		t.Fatalf("local write: %v", err)
	}

	engine := hasync.NewEngine(hasync.Config{
		NodeID:    "device-1",
		TenantID:  tenantID,
		Events:    device.OutboxStore(),
		Cursors:   device.CursorStore(),
		Inbox:     device.InboxStore(),
		Resources: device.ResourceStore(),
		History:   device.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	})

	push, err := engine.Push(ctx)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if push.Results[0].State != hasync.AckAccepted {
		t.Fatalf("push result = %+v", push.Results[0])
	}

	replica, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open replica: %v", err)
	}
	t.Cleanup(func() { _ = replica.Close() })
	if err := replica.Migrate(ctx); err != nil {
		t.Fatalf("migrate replica: %v", err)
	}

	pull, err := hasync.NewEngine(hasync.Config{
		NodeID:    "device-2",
		TenantID:  tenantID,
		Cursors:   replica.CursorStore(),
		Inbox:     replica.InboxStore(),
		Resources: replica.ResourceStore(),
		History:   replica.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	}).Pull(ctx)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if pull.Applied != 1 {
		t.Fatalf("pull = %+v", pull)
	}

	exists, err := replica.ResourceStore().Exists(ctx, "Patient", "p-sync")
	if err != nil || !exists {
		t.Fatalf("replica resource missing: exists=%v err=%v", exists, err)
	}

	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("hub events = %d err=%v", len(events), err)
	}
}

func openIntegrationPostgres(t *testing.T) (*postgres.DB, func()) {
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

	if !integrationDockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("haistack_test"),
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

	return db, func() {
		db.Close()
		_ = container.Terminate(ctx)
	}
}

func integrationDockerAvailable() bool {
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
