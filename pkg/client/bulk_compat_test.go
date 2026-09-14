//go:build bulk_compat

package client_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
)

// TestBulkExportExternalServerCompatibility exercises kickoff and manifest retrieval
// against an external conformant FHIR server when TEST_FHIR_SERVER_URL is set.
func TestBulkExportExternalServerCompatibility(t *testing.T) {
	baseURL := os.Getenv("TEST_FHIR_SERVER_URL")
	if baseURL == "" {
		t.Skip("set TEST_FHIR_SERVER_URL to run external bulk export compatibility test")
	}
	token := os.Getenv("TEST_FHIR_SERVER_TOKEN")

	cfg := client.Config{BaseURL: baseURL}
	if token != "" {
		cfg.AuthToken = token
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	job, err := c.BulkExport().Kickoff(ctx, client.ExportKickoffRequest{
		ResourceTypes: []string{"Patient"},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.StatusURL == "" {
		t.Fatal("missing status URL")
	}

	completed, err := c.BulkExport().Wait(ctx, job.StatusURL, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	manifest, err := c.BulkExport().GetManifest(ctx, completed.StatusURL)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Request == "" {
		t.Fatal("manifest missing request field")
	}
}
