package bulkimport_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/bulkimport"
)

func TestInMemoryJobStoreUpdateDoesNotUncancel(t *testing.T) {
	ctx := context.Background()
	store := bulkimport.NewInMemoryJobStore()
	job := bulkimport.Job{ID: "job-1", Status: bulkimport.StatusInProgress}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cancelled := job
	cancelled.Status = bulkimport.StatusCancelled
	cancelled.CancelRequested = true
	if err := store.Update(ctx, cancelled); err != nil {
		t.Fatalf("Update cancelled: %v", err)
	}

	complete := job
	complete.Status = bulkimport.StatusComplete
	complete.Progress = "100%"
	complete.CancelRequested = false
	if err := store.Update(ctx, complete); err != nil {
		t.Fatalf("Update complete: %v", err)
	}

	got, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected job")
	}
	if got.Status != bulkimport.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
	if !got.CancelRequested {
		t.Fatal("expected CancelRequested to remain set")
	}
}

func TestInMemoryJobStoreUpdateHonorsCancelRequested(t *testing.T) {
	ctx := context.Background()
	store := bulkimport.NewInMemoryJobStore()
	job := bulkimport.Job{
		ID:              "job-2",
		Status:          bulkimport.StatusInProgress,
		CancelRequested: true,
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	complete := job
	complete.Status = bulkimport.StatusComplete
	complete.CancelRequested = false
	if err := store.Update(ctx, complete); err != nil {
		t.Fatalf("Update complete: %v", err)
	}

	got, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != bulkimport.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
	if !got.CancelRequested {
		t.Fatal("expected CancelRequested to remain set")
	}
}
