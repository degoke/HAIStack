package search_test

import (
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestUnknownParamError(t *testing.T) {
	err := search.UnknownParamError{ResourceType: "Patient", Code: "unknown-code"}
	if !errors.Is(err, search.ErrUnknownParam) {
		t.Fatalf("expected ErrUnknownParam, got %v", err)
	}
	code, ok := search.UnknownParamCode(err)
	if !ok || code != "unknown-code" {
		t.Fatalf("UnknownParamCode = %q, ok=%v", code, ok)
	}
}

func TestServiceSearchParametersFor(t *testing.T) {
	svc, err := search.NewService(search.ServiceConfig{
		Registry:  search.NewSnapshotRegistry(testSnapshot(t, "Patient")),
		Executor:  projectionExecutor{result: &search.ExecuteResult{}},
		Resources: projectionResourceStore{resources: map[string]*types.ResourceEnvelope{}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	params := svc.SearchParametersFor("Patient")
	if len(params) == 0 {
		t.Fatal("expected patient search parameters")
	}
	types := svc.EnabledResourceTypes()
	if len(types) == 0 || types[0] != "Patient" {
		t.Fatalf("EnabledResourceTypes = %v", types)
	}
}
