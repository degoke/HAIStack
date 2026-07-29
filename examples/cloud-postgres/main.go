package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/examples/internal/postgresdemo"
	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cloud-postgres: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dsn, cleanup, err := postgresdemo.Open(ctx, "haistack_examples_cloud")
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	tenantID := fmt.Sprintf("cloud-example-%d", time.Now().UnixNano())
	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, tenantID).
		WithExternalBlobStore(runtime.TestNoopBlobStore()).
		WithExternalSearch(runtime.TestNoopExternalSearch()).
		WithExternalWarehouse(runtime.TestNoopWarehouse()).
		WithModules(appkit.RepoPath("modules", "core")).
		WithSearch().
		Build(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Mae", "Jemison", "+1-555-0150"))
	if err != nil {
		return err
	}
	created, err := rt.Services().ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	readBack, err := rt.Services().ResourceService.Read(ctx, "Patient", created.ID)
	if err != nil {
		return err
	}

	fmt.Println("cloud Postgres plus external service seams")
	fmt.Printf("mode: %s\n", rt.Mode())
	fmt.Printf("tenant: %s\n", tenantID)
	fmt.Printf("blob adapter: %s\n", rt.Services().BlobStore.Name())
	fmt.Printf("search adapter: %s\n", rt.Services().ExternalSearch.Name())
	fmt.Printf("warehouse adapter: %s\n", rt.Services().Warehouse.Name())
	fmt.Printf("created patient id: %s\n", created.ID)
	fmt.Println("read-back resource:")
	fmt.Println(appkit.PrettyJSON(readBack.JSON))
	return nil
}
