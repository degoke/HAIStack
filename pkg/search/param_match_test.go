package search_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/search"
	"github.com/degoke/haistack/pkg/types"
)

func TestMatchResourceParameter_ObservationCategory(t *testing.T) {
	ctx := context.Background()
	reg := search.NewSnapshotRegistry(testSnapshot(t, "Observation"))
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	lab := observationEnvelope(t, "obs-lab", "laboratory")
	vital := observationEnvelope(t, "obs-vital", "vital-signs")

	matched, known := search.MatchResourceParameter(ctx, reg, engine, "Observation", lab, "category", []string{"laboratory"})
	if !known || !matched {
		t.Fatalf("lab match = %v known = %v", matched, known)
	}
	matched, known = search.MatchResourceParameter(ctx, reg, engine, "Observation", vital, "category", []string{"laboratory"})
	if !known || matched {
		t.Fatalf("vital match = %v known = %v", matched, known)
	}
}

func TestMatchResourceParameter_PatientActive(t *testing.T) {
	ctx := context.Background()
	reg := search.NewSnapshotRegistry(testSnapshot(t, "Patient"))
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	active := patientActiveEnvelope(t, "pat-active", true)
	inactive := patientActiveEnvelope(t, "pat-inactive", false)

	matched, known := search.MatchResourceParameter(ctx, reg, engine, "Patient", active, "active", []string{"true"})
	if !known || !matched {
		t.Fatalf("active=true match = %v known = %v", matched, known)
	}
	matched, known = search.MatchResourceParameter(ctx, reg, engine, "Patient", inactive, "active", []string{"true"})
	if !known || matched {
		t.Fatalf("inactive should not match active=true, match = %v known = %v", matched, known)
	}
	matched, known = search.MatchResourceParameter(ctx, reg, engine, "Patient", inactive, "active", []string{"false"})
	if !known || !matched {
		t.Fatalf("active=false match = %v known = %v", matched, known)
	}
}

func observationEnvelope(t *testing.T, id, category string) *types.ResourceEnvelope {
	t.Helper()
	payload := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       "final",
		"code":         map[string]any{"text": "test"},
		"category": []map[string]any{{
			"coding": []map[string]any{{"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": category}},
		}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	codec := types.NewJSONCodec()
	envelope, err := codec.ParseJSON("Observation", data)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func patientActiveEnvelope(t *testing.T, id string, active bool) *types.ResourceEnvelope {
	t.Helper()
	payload := map[string]any{
		"resourceType": "Patient",
		"id":           id,
		"active":       active,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	codec := types.NewJSONCodec()
	envelope, err := codec.ParseJSON("Patient", data)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}
