package proto_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/proto"
	protor4 "github.com/degoke/health-ai-stack/pkg/proto/r4"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestGoogleR4Codec_ParseJSONToEnvelope(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"meta": {
			"versionId": "2",
			"lastUpdated": "2017-01-01T00:00:00.000+00:00"
		},
		"name": [{"text": "Jane"}]
	}`)

	codec := proto.NewGoogleR4Codec()
	envelope, err := codec.ParseJSONToEnvelope("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToEnvelope: %v", err)
	}
	if envelope.ResourceType != "Patient" || envelope.ID != "pat-1" {
		t.Fatalf("envelope identity = %q/%q", envelope.ResourceType, envelope.ID)
	}
	if envelope.Proto == nil || envelope.Hash == "" {
		t.Fatal("expected proto and hash on envelope")
	}

	jsonEnvelope, err := types.NewJSONCodec().ParseJSON("Patient", data)
	if err != nil {
		t.Fatalf("JSONCodec.ParseJSON: %v", err)
	}
	if envelope.Hash != jsonEnvelope.Hash {
		t.Errorf("hash = %s, want %s", envelope.Hash, jsonEnvelope.Hash)
	}
}

func TestParseJSONToEnvelope_ConvenienceHelper(t *testing.T) {
	data := []byte(`{"resourceType":"Patient","id":"pat-1"}`)
	envelope, err := proto.ParseJSONToEnvelope("", data)
	if err != nil {
		t.Fatalf("ParseJSONToEnvelope: %v", err)
	}
	if envelope.ResourceType != "Patient" || envelope.ID != "pat-1" {
		t.Fatalf("envelope identity = %q/%q", envelope.ResourceType, envelope.ID)
	}
}

func TestAsContainedResource_FromParsePath(t *testing.T) {
	data := []byte(`{"resourceType":"Patient","id":"pat-1"}`)
	codec := proto.NewGoogleR4Codec()
	envelope, err := codec.ParseJSONToEnvelope("", data)
	if err != nil {
		t.Fatalf("ParseJSONToEnvelope: %v", err)
	}

	cr, err := proto.AsContainedResource(envelope.Proto)
	if err != nil {
		t.Fatalf("AsContainedResource: %v", err)
	}
	if cr.GetPatient() == nil || cr.GetPatient().GetId().GetValue() != "pat-1" {
		t.Fatalf("unexpected patient in contained resource: %#v", cr.GetPatient())
	}
}

func TestAsContainedResource_FromIndividualResource(t *testing.T) {
	patient := protor4.NewPatient("ind-1")
	cr, err := proto.AsContainedResource(patient)
	if err != nil {
		t.Fatalf("AsContainedResource: %v", err)
	}
	if cr.GetPatient() == nil || cr.GetPatient().GetId().GetValue() != "ind-1" {
		t.Fatalf("unexpected wrapped patient: %#v", cr.GetPatient())
	}
}

func TestContainedResourceFromEnvelope(t *testing.T) {
	patient := protor4.NewPatient("pat-1")
	envelope, err := proto.ToEnvelope(patient)
	if err != nil {
		t.Fatalf("ToEnvelope: %v", err)
	}

	cr, err := proto.ContainedResourceFromEnvelope(envelope)
	if err != nil {
		t.Fatalf("ContainedResourceFromEnvelope: %v", err)
	}
	if cr.GetPatient() == nil || cr.GetPatient().GetId().GetValue() != "pat-1" {
		t.Fatalf("unexpected patient: %#v", cr.GetPatient())
	}
}

func TestContainedResourceFromEnvelope_Errors(t *testing.T) {
	if _, err := proto.ContainedResourceFromEnvelope(nil); err == nil {
		t.Fatal("expected error for nil envelope")
	}
	if _, err := proto.ContainedResourceFromEnvelope(&types.ResourceEnvelope{}); err == nil {
		t.Fatal("expected error for envelope without proto")
	}
}

func TestParseJSONToEnvelope_RejectsUnknownFields(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"vendorOnlyField": "should-not-survive"
	}`)

	codec := proto.NewGoogleR4Codec()
	_, err := codec.ParseJSONToEnvelope("", data)
	if err == nil {
		t.Fatal("expected error for unknown top-level field")
	}
}
