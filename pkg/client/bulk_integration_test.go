package client_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/export"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type exportResourceService struct{}

func (exportResourceService) Create(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (exportResourceService) Update(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (exportResourceService) Delete(context.Context, string, string) error { return nil }
func (exportResourceService) History(context.Context, string, string) ([]store.ResourceVersion, error) {
	return nil, nil
}
func (exportResourceService) ProcessTransactionBundle(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (exportResourceService) ProcessBatchBundle(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (exportResourceService) Patch(context.Context, string, string, []byte) (*types.ResourceEnvelope, error) {
	return nil, nil
}
func (exportResourceService) ListIDs(_ context.Context, resourceType string, _ int, offset int) ([]string, error) {
	if resourceType != "Patient" || offset > 0 {
		return nil, nil
	}
	return []string{"p1"}, nil
}
func (exportResourceService) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if resourceType == "Patient" && id == "p1" {
		return &types.ResourceEnvelope{
			ResourceType: "Patient",
			ID:           "p1",
			LastUpdated:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
		}, nil
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
}

func TestBulkExportClientServerRoundTrip(t *testing.T) {
	files := export.NewInMemoryFileStore()
	jobs := export.NewInMemoryJobStore()
	executor := &export.Executor{Resources: exportResourceService{}, Files: files}
	svc, err := export.NewService(export.Config{
		Jobs:     jobs,
		Files:    files,
		Executor: executor,
		BasePath: "/fhir",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:   exportResourceService{},
		BulkExportService: svc,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, err := client.New(client.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	job, err := c.BulkExport().Kickoff(context.Background(), client.ExportKickoffRequest{
		ResourceTypes: []string{"Patient"},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	manifest, err := c.BulkExport().GetManifest(context.Background(), job.StatusURL)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(manifest.Output) != 1 || manifest.Output[0].Type != "Patient" {
		t.Fatalf("manifest = %#v", manifest)
	}
}
