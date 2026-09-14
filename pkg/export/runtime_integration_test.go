package export_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestRuntimeBulkExportSelfTest(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bulk-selftest.db")
	coreModule := filepath.Join("..", "..", "modules", "core")

	rt, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:0").
		WithModules(coreModule).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rt.Services().BulkExportService == nil {
		t.Fatal("expected bulk export service")
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := rt.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}()

	base := "http://" + rt.HTTPAddr().String()
	c, err := client.New(client.Config{BaseURL: base})
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	job, err := c.BulkExport().Kickoff(ctx, client.ExportKickoffRequest{
		ResourceTypes: []string{"Patient"},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	completed, err := c.BulkExport().Wait(ctx, job.StatusURL, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	manifest, err := c.BulkExport().GetManifest(ctx, completed.StatusURL)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Request == "" {
		t.Fatal("manifest missing request")
	}

	// Verify HTTP layer returns 202 on kickoff semantics via client (already validated above).
	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
	_ = time.Now()
}
