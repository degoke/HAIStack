package view_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestLocalExportFileStoreRejectsPathTraversalFilename(t *testing.T) {
	ctx := context.Background()
	store, err := view.NewLocalExportFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalExportFileStore: %v", err)
	}
	err = store.Put(ctx, "job-1", "../../../etc/passwd", []byte("secret"), "text/plain")
	if err == nil {
		t.Fatal("expected path traversal filename to be rejected")
	}
}

func TestLocalExportFileStoreRejectsPathTraversalJobID(t *testing.T) {
	ctx := context.Background()
	store, err := view.NewLocalExportFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalExportFileStore: %v", err)
	}
	err = store.Put(ctx, "../other-job", "out.ndjson", []byte("row"), "application/fhir+ndjson")
	if err == nil {
		t.Fatal("expected path traversal job id to be rejected")
	}
}

func TestLocalViewExportJobStoreRejectsPathTraversalJobID(t *testing.T) {
	ctx := context.Background()
	store, err := view.NewLocalViewExportJobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalViewExportJobStore: %v", err)
	}
	err = store.Create(ctx, view.ViewExportJob{ID: "../escape"})
	if err == nil {
		t.Fatal("expected path traversal job id to be rejected")
	}
}
