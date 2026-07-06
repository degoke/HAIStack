package search_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
)

func TestSnapshotRegistryEnabledParams(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	if !reg.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled")
	}
	params := reg.SearchParametersFor("Patient")
	if len(params) == 0 {
		t.Fatal("expected Patient search parameters")
	}

	codes := make(map[string]struct{})
	for _, p := range params {
		codes[p.Code] = struct{}{}
	}
	for _, want := range []string{"name", "identifier", "birthdate", "phone", "gender"} {
		if _, ok := codes[want]; !ok {
			t.Fatalf("missing param %q", want)
		}
	}
}

func TestSnapshotRegistryUnknownParamRejected(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	_, ok := reg.SearchParameter("Patient", "not-a-real-param")
	if ok {
		t.Fatal("unknown param should not resolve")
	}
}

func TestSnapshotRegistryDisabledType(t *testing.T) {
	snapshot := testSnapshot(t)
	reg := search.NewSnapshotRegistry(snapshot)
	if reg.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be disabled")
	}
	if params := reg.SearchParametersFor("Patient"); params != nil {
		t.Fatalf("expected nil params, got %d", len(params))
	}
}

func TestSnapshotRegistryReferenceTargets(t *testing.T) {
	snapshot := testSnapshot(t, "Observation")
	reg := search.NewSnapshotRegistry(snapshot)
	info, ok := reg.SearchParameter("Observation", "subject")
	if !ok {
		t.Fatal("expected subject param")
	}
	if len(info.Target) == 0 {
		t.Fatal("expected reference targets")
	}
}
