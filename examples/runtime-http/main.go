package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "runtime-http: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-runtime-http-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	rt, err := runtime.New().
		WithSQLite(filepath.Join(tempDir, "runtime.db")).
		WithModules(appkit.RepoPath("modules", "core")).
		WithSearch().
		WithHTTP("127.0.0.1:0").
		Build(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Katherine", "Johnson", "+1-555-0110"))
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
	metadataBody, err := get(baseURL + "/fhir/metadata")
	if err != nil {
		return err
	}
	patientBody, err := get(baseURL + "/fhir/Patient/" + created.ID)
	if err != nil {
		return err
	}
	searchBody, err := get(baseURL + "/fhir/Patient?name=Katherine")
	if err != nil {
		return err
	}

	fmt.Println("runtime-managed HTTP composition")
	fmt.Printf("http address: %s\n", baseURL)
	fmt.Println("GET /fhir/metadata")
	fmt.Println(appkit.PrettyJSON(metadataBody))
	fmt.Printf("GET /fhir/Patient/%s\n", created.ID)
	fmt.Println(appkit.PrettyJSON(patientBody))
	fmt.Println("GET /fhir/Patient?name=Katherine")
	fmt.Println(appkit.PrettyJSON(searchBody))
	return rt.Shutdown(ctx)
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
