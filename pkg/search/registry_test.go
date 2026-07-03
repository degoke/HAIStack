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
	for _, want := range []string{"name", "identifier", "birthdate", "phone"} {
		if _, ok := codes[want]; !ok {
			t.Fatalf("missing supported param %q", want)
		}
	}
	if _, ok := codes["gender"]; ok {
		t.Fatal("gender should not be exposed as a supported param")
	}
}

func TestSnapshotRegistryUnknownParamRejected(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	_, ok := reg.SearchParameter("Patient", "gender")
	if ok {
		t.Fatal("gender should not be available through the supported-param registry wrapper")
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
