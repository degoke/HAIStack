package jobs

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestTerminologyPreExpandWorkerRequiresPackPayload(t *testing.T) {
	worker := &TerminologyPreExpandWorker{
		Terminology: &stubTerminologyStore{},
		Definitions: &stubDefinitionStore{},
		TenantScope: "tenant-a",
	}
	payload, err := MarshalPayload(TerminologyPreExpandPayload{ScopeID: "__global__"})
	if err != nil {
		t.Fatal(err)
	}
	err = worker.HandleJob(context.Background(), store.JobRecord{Payload: payload})
	if err == nil || err.Error() != "packName and packVersion are required for terminology pre-expand" {
		t.Fatalf("err=%v", err)
	}
}

type stubTerminologyStore struct{}

func (s *stubTerminologyStore) FindResource(context.Context, string, string, string, string) (*store.TerminologyResourceRecord, error) {
	return nil, nil
}
func (s *stubTerminologyStore) PutResource(context.Context, store.TerminologyResourceRecord) error { return nil }
func (s *stubTerminologyStore) DeleteResource(context.Context, string, string, string, string) error {
	return nil
}
func (s *stubTerminologyStore) ListResources(context.Context, string, string) ([]store.TerminologyResourceRecord, error) {
	return nil, nil
}
func (s *stubTerminologyStore) ReplaceCodeSystem(context.Context, string, string, string, []store.TerminologyConceptRecord) error {
	return nil
}
func (s *stubTerminologyStore) LookupConcept(context.Context, string, string, string, string) (*store.TerminologyConceptRecord, error) {
	return nil, nil
}
func (s *stubTerminologyStore) ReplaceValueSet(context.Context, store.TerminologyValueSetRecord, []store.TerminologyExpansionMemberRecord) error {
	return nil
}
func (s *stubTerminologyStore) GetValueSet(context.Context, string, string, string) (*store.TerminologyValueSetRecord, error) {
	return nil, nil
}
func (s *stubTerminologyStore) ListValueSetMembers(context.Context, string, string, string) ([]store.TerminologyExpansionMemberRecord, error) {
	return nil, nil
}
func (s *stubTerminologyStore) DeleteProjections(context.Context, string, string, string, string) error {
	return nil
}

type stubDefinitionStore struct{}

func (s *stubDefinitionStore) Upsert(context.Context, store.DefinitionResourceRecord, []store.DefinitionTargetRecord) error {
	return nil
}
func (s *stubDefinitionStore) Get(context.Context, string, string) (*store.DefinitionResourceRecord, error) {
	return nil, nil
}
func (s *stubDefinitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	return nil, nil
}
func (s *stubDefinitionStore) Delete(context.Context, string, string) error { return nil }
