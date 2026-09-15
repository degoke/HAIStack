package client_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
)

func TestParseScopes_CRUDS(t *testing.T) {
	set, err := client.ParseScopes("user/Patient.cruds")
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() != 1 {
		t.Fatalf("len = %d", set.Len())
	}
}

func TestPatientObservationFiltered(t *testing.T) {
	scope, err := client.PatientObservationFiltered("laboratory")
	if err != nil {
		t.Fatal(err)
	}
	if scope == "" {
		t.Fatal("expected scope string")
	}
}

func TestJoinScopes(t *testing.T) {
	joined, err := client.JoinScopes("openid", "patient/Observation.rs?category=laboratory")
	if err != nil {
		t.Fatal(err)
	}
	if joined == "" {
		t.Fatal("expected joined scopes")
	}
}
