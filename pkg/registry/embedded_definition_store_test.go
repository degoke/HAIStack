package registry

import (
	"context"
	"testing"
)

func TestEmbeddedDefinitionStoreLoadsPatientStructureDefinition(t *testing.T) {
	store := EmbeddedDefinitionStore()
	record, err := store.Get(context.Background(), "http://hl7.org/fhir/StructureDefinition/Patient", "")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || len(record.JSONData) == 0 {
		t.Fatal("expected embedded Patient StructureDefinition")
	}
}
