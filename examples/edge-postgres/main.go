package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/examples/internal/postgresdemo"
	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "edge-postgres: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dsn, cleanup, err := postgresdemo.Open(ctx, "haistack_examples_edge")
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	tenantID := fmt.Sprintf("edge-example-%d", time.Now().UnixNano())
	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, tenantID).
		WithModules(appkit.RepoPath("modules", "core")).
		WithSearch().
		WithHTTP("127.0.0.1:0").
		Build(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = appkit.ShutdownRuntime(ctx, rt) }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Patricia", "Bath", "+1-555-0140"))
	if err != nil {
		return err
	}
	created, err := rt.Services().ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	if err := rt.Start(ctx); err != nil {
		return err
	}

	baseURL := "http://" + rt.HTTPAddr().String()
	searchBody, err := get(baseURL + "/fhir/Patient?name=Patricia")
	if err != nil {
		return err
	}

	fmt.Println("edge Postgres all-in-one runtime")
	fmt.Printf("mode: %s\n", rt.Mode())
	fmt.Printf("tenant: %s\n", tenantID)
	fmt.Printf("created patient id: %s\n", created.ID)
	fmt.Println("GET /fhir/Patient?name=Patricia")
	fmt.Println(appkit.PrettyJSON(searchBody))
	return appkit.ShutdownRuntime(ctx, rt)
}

func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}
