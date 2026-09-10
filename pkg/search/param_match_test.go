package search_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
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
