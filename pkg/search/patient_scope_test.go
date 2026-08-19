package search_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
)

func TestResolvedPatientSearchParamIsIndexed(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Observation")
	code, ok := snapshot.PatientSearchParameterCode("Observation")
	if !ok {
		t.Fatal("expected patient search param code")
	}
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
	fieldKey := "reference." + code
	if len(values[fieldKey]) == 0 {
		t.Fatalf("resolved param %q missing index key %q; values=%#v", code, fieldKey, values)
	}
}
