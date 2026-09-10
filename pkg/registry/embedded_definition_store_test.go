package registry

import (
	"context"
	"testing"
	"time"
)

func TestEmbeddedDefinitionStoreLoadsPatientStructureDefinition(t *testing.T) {
	store := EmbeddedDefinitionStore()
	start := time.Now()
	record, err := store.Get(context.Background(), "http://hl7.org/fhir/StructureDefinition/Patient", "")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || len(record.JSONData) == 0 {
		t.Fatal("expected embedded Patient StructureDefinition")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("expected lazy Patient load, took %s", elapsed)
	}
}

func TestEmbeddedDefinitionStoreLoadsOnDemand(t *testing.T) {
	store := EmbeddedDefinitionStore()
	_, err := store.Get(context.Background(), "http://hl7.org/fhir/StructureDefinition/Observation", "")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := store.Get(context.Background(), "http://example.org/StructureDefinition/custom", "")
	if err == nil || missing != nil {
		t.Fatalf("expected missing custom profile, got err=%v record=%v", err, missing)
	}
}
