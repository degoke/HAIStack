package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestSQLiteJobStoreEnqueueClaimUpdateGet(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	jobs := db.JobStore()

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	job, err := NewJob(TypeReindex, map[string]string{"rt": "Patient"}, EnqueueOptions{
		ID:  "sqlite-job-1",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := jobs.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := jobs.ClaimNext(ctx, TypeReindex)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claim")
	}
	if claimed.Status != store.JobStatusRunning || claimed.Attempts != 1 {
		t.Fatalf("claimed = %#v", claimed)
	}

	none, err := jobs.ClaimNext(ctx, TypeReindex)
	if err != nil || none != nil {
		t.Fatalf("second claim = %#v err=%v", none, err)
	}

	MarkCompleted(claimed, now)
	if err := jobs.Update(ctx, *claimed); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := jobs.Get(ctx, "sqlite-job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != store.JobStatusCompleted {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestClaimSemanticsParityMemoryAndSQLite(t *testing.T) {
	ctx := context.Background()
	wall := time.Now().UTC()

	mem := NewInMemoryJobStore()
	mem.Now = func() time.Time { return wall }
	sqlDB := openSQLite(t)
	sqlJobs := sqlDB.JobStore()

	stores := []struct {
		name  string
		store store.JobStore
	}{
		{"memory", mem},
		{"sqlite", sqlJobs},
	}

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			id := "parity-" + tc.name
			job, err := NewJob("parity.type", nil, EnqueueOptions{
				ID:       id,
				RunAfter: wall.Add(24 * time.Hour),
				Now:      func() time.Time { return wall },
			})
			if err != nil {
				t.Fatalf("NewJob: %v", err)
			}
			if err := tc.store.Enqueue(ctx, job); err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			claimed, err := tc.store.ClaimNext(ctx, "parity.type")
			if err != nil {
				t.Fatalf("ClaimNext early: %v", err)
			}
			if claimed != nil {
				t.Fatalf("expected nil before RunAfter, got %#v", claimed)
			}

			job.RunAfter = wall.Add(-time.Second)
			job.UpdatedAt = wall
			if err := tc.store.Update(ctx, job); err != nil {
				t.Fatalf("Update run_after: %v", err)
			}
			claimed, err = tc.store.ClaimNext(ctx, "parity.type")
			if err != nil || claimed == nil {
				t.Fatalf("ClaimNext: %#v err=%v", claimed, err)
			}
			if claimed.Attempts != 1 || claimed.Status != store.JobStatusRunning {
				t.Fatalf("claim semantics = %#v", claimed)
			}
		})
	}
}

func openSQLite(t *testing.T) *sqlite.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}
