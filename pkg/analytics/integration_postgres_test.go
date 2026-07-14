package analytics_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

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

	if !dockerAvailable() {
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

	cleanup := func() {
		db.Close()
		_ = container.Terminate(ctx)
	}
	return db, cleanup
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

func testTenant(t *testing.T, db *postgres.DB, suffix string) *postgres.TenantDB {
	t.Helper()
	tenantID := fmt.Sprintf("analytics-%s-%s", t.Name(), suffix)
	if err := db.EnsureTenant(context.Background(), tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	return db.Tenant(tenantID)
}

func TestPostgresReportingTable_FullRefreshPatientSummary(t *testing.T) {
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "refresh")

	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), patientJohn(t))
	exec := newTestExecutor(t, resources)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec, Now: fixedNow()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	reporting := tdb.ReportingTableStore()
	target := analytics.NewReportingTarget(reporting)

	result, err := runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: target,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RowCount != 2 {
		t.Fatalf("RowCount = %d, want 2", result.RowCount)
	}

	rows, err := reporting.QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	meta, err := reporting.GetMeta(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.RowCount != 2 {
		t.Fatalf("meta.RowCount = %d, want 2", meta.RowCount)
	}
	if len(meta.Columns) != 5 {
		t.Fatalf("len(meta.Columns) = %d, want 5", len(meta.Columns))
	}
}

func TestPostgresReportingTable_TenantIsolation(t *testing.T) {
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := testTenant(t, db, "a")
	tenantB := testTenant(t, db, "b")

	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec := newTestExecutor(t, resources)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec, Now: fixedNow()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(tenantA.ReportingTableStore()),
		},
	})
	if err != nil {
		t.Fatalf("Run tenant A: %v", err)
	}

	rowsB, err := tenantB.ReportingTableStore().QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows tenant B: %v", err)
	}
	if len(rowsB) != 0 {
		t.Fatalf("tenant B rows = %d, want 0", len(rowsB))
	}
}

func TestPostgresReportingTable_IdempotentRefresh(t *testing.T) {
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "idempotent")

	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec := newTestExecutor(t, resources)
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec, Now: fixedNow()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	req := analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(tdb.ReportingTableStore()),
		},
	}

	if _, err := runner.Run(ctx, req); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first, err := tdb.ReportingTableStore().QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows first: %v", err)
	}

	if _, err := runner.Run(ctx, req); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second, err := tdb.ReportingTableStore().QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows second: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("row count changed: first=%d second=%d", len(first), len(second))
	}
	if first[0]["id"] != second[0]["id"] {
		t.Fatalf("row content changed: first=%v second=%v", first[0], second[0])
	}
}

func TestPostgresReportingTable_EmptyResultSet(t *testing.T) {
	db, cleanup := openPostgresTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "empty")

	exec := newTestExecutor(t, newMemResourceStore())
	runner, err := analytics.NewRunner(analytics.Config{Executor: exec, Now: fixedNow()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(ctx, analytics.RunRequest{
		ViewName: analytics.ViewPatientSummary,
		Mode:     analytics.ModeRefresh,
		Destination: analytics.Destination{
			Reporting: analytics.NewReportingTarget(tdb.ReportingTableStore()),
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RowCount != 0 {
		t.Fatalf("RowCount = %d, want 0", result.RowCount)
	}

	rows, err := tdb.ReportingTableStore().QueryRows(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("QueryRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}

	meta, err := tdb.ReportingTableStore().GetMeta(ctx, analytics.ViewPatientSummary, "1.0.0")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.RowCount != 0 {
		t.Fatalf("meta.RowCount = %d, want 0", meta.RowCount)
	}
}
