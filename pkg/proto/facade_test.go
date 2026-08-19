package proto_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/proto"
	protor4 "github.com/degoke/health-ai-stack/pkg/proto/r4"
)

func TestR4FacadeAndConvenienceHelpers(t *testing.T) {
	patient := protor4.NewPatient("pat-1")

	resourceType, err := proto.ResourceType(patient)
	if err != nil {
		t.Fatalf("ResourceType: %v", err)
	}
	if resourceType != "Patient" {
		t.Fatalf("ResourceType = %q, want Patient", resourceType)
	}

	jsonBytes, err := proto.ToJSON(patient)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		t.Fatalf("ToJSON produced invalid JSON: %v", err)
	}
	if payload["resourceType"] != "Patient" || payload["id"] != "pat-1" {
		t.Fatalf("ToJSON payload = %#v", payload)
	}

	envelope, err := proto.ToEnvelope(patient)
	if err != nil {
		t.Fatalf("ToEnvelope: %v", err)
	}
	if envelope.ResourceType != "Patient" || envelope.ID != "pat-1" {
		t.Fatalf("envelope identity = %q/%q", envelope.ResourceType, envelope.ID)
	}
	if len(envelope.JSON) == 0 || envelope.Hash == "" {
		t.Fatalf("envelope missing canonical JSON or hash: %#v", envelope)
	}
	if canonical, err := proto.ToJSON(patient); err != nil || !bytes.Equal(envelope.JSON, canonical) {
		t.Fatalf("envelope JSON is not the same canonical output as ToJSON: err=%v", err)
	}
	if envelope.Proto != patient {
		t.Fatal("ToEnvelope did not retain the original proto value")
	}

	// These aliases cover a resource, a shared datatype, an enum-bearing nested
	// message, and another resource package at compile time.
	var _ = &protor4.Observation{}
	var _ = &protor4.Bundle{}
	var _ = &protor4.ContainedResource{}
	var _ = &protor4.Observation_StatusCode{}
	var _ = protor4.ObservationStatusCode_FINAL
}

func TestConvenienceHelpersSupportObservation(t *testing.T) {
	observation := protor4.NewObservation("obs-1", "Heart rate")

	envelope, err := proto.ToEnvelope(observation)
	if err != nil {
		t.Fatalf("ToEnvelope(Observation): %v", err)
	}
	if envelope.ResourceType != "Observation" || envelope.ID != "obs-1" {
		t.Fatalf("envelope identity = %q/%q", envelope.ResourceType, envelope.ID)
	}
}

func TestConvenienceHelpersRejectInvalidValues(t *testing.T) {
	var nilPatient *protor4.Patient
	for _, value := range []any{nil, nilPatient, 42} {
		if _, err := proto.ToEnvelope(value); err == nil {
			t.Errorf("ToEnvelope(%T) returned nil error", value)
		}
		if _, err := proto.ToJSON(value); err == nil {
			t.Errorf("ToJSON(%T) returned nil error", value)
		}
		if _, err := proto.ResourceType(value); err == nil {
			t.Errorf("ResourceType(%T) returned nil error", value)
		}
	}

	codec := proto.NewGoogleR4Codec()
	_, err := codec.ProtoToJSON("Observation", &protor4.Patient{})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("mismatched ProtoToJSON error = %v", err)
	}
}
