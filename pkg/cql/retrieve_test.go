package cql

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type testRefLookup struct {
	ids map[string][]string
}

func (t testRefLookup) LookupReferencing(_ context.Context, sourceType, fieldKey, targetType, targetID string) ([]string, error) {
	return t.ids[sourceType+"|"+fieldKey+"|"+targetType+"|"+targetID], nil
}

func TestStoreRetrieverUsesSubjectLookup(t *testing.T) {
	mine, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "mine",
		"status": "final",
		"code": {"text": "HR"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	other, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "other",
		"status": "final",
		"code": {"text": "HR"},
		"subject": {"reference": "Patient/other"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Observation": {"mine": mine, "other": other},
	}}
	retriever := StoreRetriever{
		Resources: store,
		References: testRefLookup{ids: map[string][]string{
			"Observation|reference.subject|Patient|ada": {"mine"},
		}},
	}
	eng, err := NewEngine(Config{
		Retriever: retriever,
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "[Observation].count()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("subject lookup count: %#v", got)
	}
	if store.reads != 1 {
		t.Fatalf("expected one Observation read via subject lookup, got %d", store.reads)
	}
}

func TestStoreRetrieverPatientReadsByID(t *testing.T) {
	pat := adaPatient(t)
	other, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{"resourceType":"Patient","id":"zzz"}`))
	if err != nil {
		t.Fatal(err)
	}
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Patient": {"ada": pat, "zzz": other},
	}}
	retriever := StoreRetriever{Resources: store}
	items, err := retriever.Retrieve(context.Background(), RetrieveRequest{ResourceType: "Patient"}, pat)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected single patient, got %d", len(items))
	}
	if store.reads != 1 {
		t.Fatalf("expected Patient/{id} read, got %d reads", store.reads)
	}
}
