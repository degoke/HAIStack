package registry

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestChainedDefinitionStoreFallsBackToEmbeddedPatient(t *testing.T) {
	store := DefinitionStoreWithEmbeddedBase(emptyDefinitionStore{})
	record, err := store.Get(context.Background(), "http://hl7.org/fhir/StructureDefinition/Patient", "")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || len(record.JSONData) == 0 {
		t.Fatal("expected embedded Patient StructureDefinition via chained store")
	}
}

type emptyDefinitionStore struct{}

func (emptyDefinitionStore) Upsert(context.Context, store.DefinitionResourceRecord, []store.DefinitionTargetRecord) error {
	return nil
}
func (emptyDefinitionStore) Delete(context.Context, string, string) error { return nil }
func (emptyDefinitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	return nil, nil
}
func (emptyDefinitionStore) Get(context.Context, string, string) (*store.DefinitionResourceRecord, error) {
	return nil, context.Canceled
}
