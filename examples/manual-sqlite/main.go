package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "manual-sqlite: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-manual-sqlite-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stack, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "manual.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	ada, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Ada", "Lovelace", "+1-555-0101"))
	if err != nil {
		return err
	}
	grace, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Grace", "Hopper", "+1-555-0102"))
	if err != nil {
		return err
	}

	createdAda, err := stack.ResourceService.Create(ctx, ada)
	if err != nil {
		return err
	}
	createdGrace, err := stack.ResourceService.Create(ctx, grace)
	if err != nil {
		return err
	}

	result, err := stack.SearchService.Search(ctx, "Patient", url.Values{"name": {"Ada"}})
	if err != nil {
		return err
	}

	fmt.Println("manual SQLite composition")
	fmt.Printf("database: %s\n", filepath.Join(tempDir, "manual.db"))
	fmt.Printf("created patient ids: %s, %s\n", createdAda.ID, createdGrace.ID)
	fmt.Printf("search total for name=Ada: %d\n", len(result.Resources))
	if len(result.Resources) > 0 {
		fmt.Println("matched resource:")
		fmt.Println(appkit.PrettyJSON(result.Resources[0].JSON))
	}
	return nil
}
