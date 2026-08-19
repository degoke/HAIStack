package r4_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/proto"
	protor4 "github.com/degoke/health-ai-stack/pkg/proto/r4"
)

func TestNewPatient(t *testing.T) {
	patient := protor4.NewPatient("pat-1")
	if patient.GetId().GetValue() != "pat-1" {
		t.Fatalf("id = %q, want pat-1", patient.GetId().GetValue())
	}

	jsonBytes, err := proto.ToJSON(patient)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if string(jsonBytes) != `{"id":"pat-1","resourceType":"Patient"}` {
		t.Fatalf("ToJSON = %s", jsonBytes)
	}
}

func TestNewObservation(t *testing.T) {
	observation := protor4.NewObservation("obs-1", "Heart rate")
	if observation.GetId().GetValue() != "obs-1" {
		t.Fatalf("id = %q, want obs-1", observation.GetId().GetValue())
	}
	if observation.GetCode().GetText().GetValue() != "Heart rate" {
		t.Fatalf("code text = %q", observation.GetCode().GetText().GetValue())
	}
	if observation.GetStatus().GetValue() != protor4.ObservationStatusCode_FINAL {
		t.Fatalf("status = %v, want final", observation.GetStatus().GetValue())
	}
}

func TestNewIdAndNewString_Empty(t *testing.T) {
	if protor4.NewId("") != nil {
		t.Fatal("NewId(\"\") should return nil")
	}
	if protor4.NewString("") != nil {
		t.Fatal("NewString(\"\") should return nil")
	}
}
