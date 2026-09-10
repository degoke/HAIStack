package view_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestLocalViewExportJobStorePersistsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	createdAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	job := view.ViewExportJob{
		ID:        "export-1",
		Status:    view.ExportComplete,
		CreatedAt: createdAt,
		Request: view.ViewExportRequest{
			Views: []view.ViewExportTarget{{ViewName: "patient_summary_view", Version: "1.0.0"}},
		},
	}

	storeA, err := view.NewLocalViewExportJobStore(dir)
	if err != nil {
		t.Fatalf("NewLocalViewExportJobStore: %v", err)
	}
	if err := storeA.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storeB, err := view.NewLocalViewExportJobStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	got, err := storeB.Get(ctx, "export-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != view.ExportComplete {
		t.Fatalf("status=%q", got.Status)
	}
	if got.Request.Views[0].ViewName != "patient_summary_view" {
		t.Fatalf("viewName=%q", got.Request.Views[0].ViewName)
	}

	job.Progress = "done"
	if err := storeB.Update(ctx, job); err != nil {
		t.Fatalf("Update: %v", err)
	}
	path := filepath.Join(dir, "export-1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected JSON job file on disk")
	}
}

func TestLocalMaterializeJobStorePersistsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	job := view.MaterializeJob{
		ID:     "mat-1",
		Status: view.MaterializeComplete,
		Request: view.MaterializeRequest{
			ViewName: "patient_summary_view",
			Version:  "1.0.0",
		},
	}

	storeA, err := view.NewLocalMaterializeJobStore(dir)
	if err != nil {
		t.Fatalf("NewLocalMaterializeJobStore: %v", err)
	}
	if err := storeA.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	storeB, err := view.NewLocalMaterializeJobStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	got, err := storeB.Get(ctx, "mat-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Request.ViewName != "patient_summary_view" {
		t.Fatalf("viewName=%q", got.Request.ViewName)
	}
}
