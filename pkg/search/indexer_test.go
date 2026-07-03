package search_test

import (
	"context"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
)

func TestRegistryIndexerPatientFields(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}

	env := patientResource(t, "pat-1", "Doe", "555-0100")
	entries, err := indexer.Build(ctx, env)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	values := fieldValues(entries)
	if !containsValue(values, "token._id", "pat-1") {
		t.Fatalf("missing _id index: %#v", values)
	}
	if !containsValue(values, "string.name", "Doe") {
		t.Fatalf("missing name index: %#v", values)
	}
	if !containsValue(values, "string.phone", "555-0100") {
		t.Fatalf("missing phone index: %#v", values)
	}
	if !containsValue(values, "date.birthdate", "1990-05-15") {
		t.Fatalf("missing birthdate index: %#v", values)
	}
	if !containsValue(values, "token.identifier", "MRN-1") {
		t.Fatalf("missing identifier index: %#v", values)
	}
	foundLastUpdated := false
	for _, v := range values["date._lastUpdated"] {
		if strings.HasPrefix(v, "2024-06-01T12:00:00") {
			foundLastUpdated = true
			break
		}
	}
	if !foundLastUpdated {
		t.Fatalf("missing _lastUpdated index: %#v", values)
	}
}

func TestRegistryIndexerObservationFields(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Observation")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}

	entries, err := indexer.Build(ctx, observationResource(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	values := fieldValues(entries)
	if !containsValue(values, "reference.subject", "pat-1") {
		t.Fatalf("missing subject index: %#v", values)
	}
	if !containsValue(values, "reference.encounter", "enc-1") {
		t.Fatalf("missing encounter index: %#v", values)
	}
	if !containsValue(values, "token.status", "final") {
		t.Fatalf("missing status index: %#v", values)
	}
	if !containsValue(values, "token.code", "8867-4") {
		t.Fatalf("missing code index: %#v", values)
	}
	if !containsValue(values, "date.date", "2024-06-01") {
		t.Fatalf("missing date index: %#v", values)
	}
}

func TestRegistryIndexerMissingFieldsNoRows(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}

	env := patientResource(t, "pat-2", "", "")
	entries, err := indexer.Build(ctx, env)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	values := fieldValues(entries)
	if containsValue(values, "string.name", "") {
		t.Fatal("expected no empty name rows")
	}
	if !containsValue(values, "token._id", "pat-2") {
		t.Fatalf("expected _id row: %#v", values)
	}
}
