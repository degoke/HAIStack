package proto_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/proto"
	"github.com/degoke/haistack/pkg/types"
)

func newCodec() *proto.GoogleR4Codec {
	return proto.NewGoogleR4Codec()
}

func TestGoogleR4Codec_ParsePatient(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"meta": {
			"versionId": "2",
			"lastUpdated": "2017-01-01T00:00:00.000+00:00"
		},
		"name": [{"text": "Jane"}]
	}`)

	codec := newCodec()
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	if !proto.IsProtoResource(pb) {
		t.Fatal("expected parsed value to be a proto resource")
	}
	rt, err := proto.ResourceTypeOfProto(pb)
	if err != nil {
		t.Fatalf("ResourceTypeOfProto: %v", err)
	}
	if rt != "Patient" {
		t.Errorf("ResourceTypeOfProto = %q, want Patient", rt)
	}
}

func TestGoogleR4Codec_ParseObservation(t *testing.T) {
	data := []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"Heart rate"}}`)
	codec := newCodec()
	pb, err := codec.ParseJSONToProto("", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	rt, err := proto.ResourceTypeOfProto(pb)
	if err != nil {
		t.Fatalf("ResourceTypeOfProto: %v", err)
	}
	if rt != "Observation" {
		t.Errorf("ResourceTypeOfProto = %q, want Observation", rt)
	}
}

func TestGoogleR4Codec_RejectInvalidJSON(t *testing.T) {
	codec := newCodec()
	_, err := codec.ParseJSONToProto("", []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGoogleR4Codec_RejectMissingResourceType(t *testing.T) {
	codec := newCodec()
	_, err := codec.ParseJSONToProto("", []byte(`{"id":"x"}`))
	if err == nil {
		t.Fatal("expected error for missing resourceType")
	}
}

func TestGoogleR4Codec_RejectMismatchedResourceType(t *testing.T) {
	codec := newCodec()
	_, err := codec.ParseJSONToProto("Patient", []byte(`{"resourceType":"Observation","id":"1"}`))
	if err == nil {
		t.Fatal("expected error for resourceType mismatch")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error = %v, want mismatch", err)
	}
}

func TestGoogleR4Codec_ProtoToJSON_SemanticEquivalence(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{
			name: "Patient",
			data: []byte(`{
				"resourceType": "Patient",
				"id": "pat-1",
				"meta": {
					"versionId": "2",
					"lastUpdated": "2017-01-01T00:00:00.000+00:00"
				},
				"name": [{"text": "Jane"}]
			}`),
		},
		{
			name: "Observation",
			data: []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"Heart rate"}}`),
		},
	}

	codec := newCodec()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pb, err := codec.ParseJSONToProto("", tc.data)
			if err != nil {
				t.Fatalf("ParseJSONToProto: %v", err)
			}

			out, err := codec.ProtoToJSON("", pb)
			if err != nil {
				t.Fatalf("ProtoToJSON: %v", err)
			}

			normIn, err := types.NormalizeJSON(tc.data)
			if err != nil {
				t.Fatalf("NormalizeJSON input: %v", err)
			}
			if string(out) != string(normIn) {
				if !jsonSemanticallyEqual(normIn, out) {
					t.Errorf("round-trip JSON differs:\ninput:  %s\noutput: %s", normIn, out)
				}
			}
		})
	}
}

func TestGoogleR4Codec_ProtoToEnvelope(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"meta": {
			"versionId": "2",
			"lastUpdated": "2017-01-01T00:00:00.000+00:00"
		},
		"name": [{"text": "Jane"}]
	}`)

	codec := newCodec()
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}

	envelope, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	if envelope.ResourceType != "Patient" {
		t.Errorf("ResourceType = %q, want Patient", envelope.ResourceType)
	}
	if envelope.ID != "pat-1" {
		t.Errorf("ID = %q, want pat-1", envelope.ID)
	}
	if envelope.VersionID != "2" {
		t.Errorf("VersionID = %q, want 2", envelope.VersionID)
	}
	if envelope.LastUpdated.IsZero() {
		t.Error("LastUpdated should be set")
	}
	if envelope.Hash == "" {
		t.Error("Hash should be set")
	}
	if envelope.Proto == nil {
		t.Fatal("Proto should be attached")
	}
	if !proto.IsProtoResource(envelope.Proto) {
		t.Error("attached Proto should be a supported resource")
	}
}

func TestGoogleR4Codec_EnvelopeHashMatchesJSONCodec(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"meta": {"versionId": "2", "lastUpdated": "2017-01-01T00:00:00.000+00:00"},
		"name": [{"text": "Jane"}]
	}`)

	jsonCodec := types.NewJSONCodec()
	jsonEnvelope, err := jsonCodec.ParseJSON("Patient", data)
	if err != nil {
		t.Fatalf("JSONCodec.ParseJSON: %v", err)
	}

	codec := newCodec()
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	protoEnvelope, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}

	if protoEnvelope.Hash != jsonEnvelope.Hash {
		t.Errorf("hash mismatch: proto=%s json=%s", protoEnvelope.Hash, jsonEnvelope.Hash)
	}
	if string(protoEnvelope.JSON) != string(jsonEnvelope.JSON) {
		t.Errorf("JSON mismatch:\nproto=%s\njson=%s", protoEnvelope.JSON, jsonEnvelope.JSON)
	}
}

func TestGoogleR4Codec_RejectUnsupportedProto(t *testing.T) {
	codec := newCodec()
	_, err := codec.ProtoToJSON("", "not-a-proto")
	if err == nil {
		t.Fatal("expected error for non-proto value")
	}
	_, err = codec.ProtoToEnvelope("", 42)
	if err == nil {
		t.Fatal("expected error for unsupported proto input")
	}
}

func TestGoogleR4Codec_ProtoToJSON_RejectsMismatchedResourceType(t *testing.T) {
	data := []byte(`{"resourceType":"Patient","id":"1"}`)
	codec := newCodec()
	pb, err := codec.ParseJSONToProto("", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	_, err = codec.ProtoToJSON("Observation", pb)
	if err == nil {
		t.Fatal("expected error for resourceType mismatch on ProtoToJSON")
	}
}

func TestGoogleR4Codec_ReferencesAndMetaRoundTrip(t *testing.T) {
	data := []byte(`{
		"resourceType": "Observation",
		"id": "obs-1",
		"status": "final",
		"meta": {
			"versionId": "7",
			"lastUpdated": "2018-06-01T12:00:00.000+00:00"
		},
		"code": {"text": "Heart rate"},
		"subject": {"reference": "Patient/123"},
		"performer": [
			{"reference": "Practitioner/abc"},
			{"reference": "urn:uuid:550e8400-e29b-41d4-a716-446655440000"}
		],
		"basedOn": [{"reference": "http://example.com/fhir/CarePlan/99"}],
		"hasMember": [{"reference": "Observation/child-1"}],
		"component": [{
			"code": {"text": "nested"},
			"valueString": "72"
		}]
	}`)

	codec := newCodec()
	pb, err := codec.ParseJSONToProto("Observation", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}

	out, err := codec.ProtoToJSON("Observation", pb)
	if err != nil {
		t.Fatalf("ProtoToJSON: %v", err)
	}

	meta, err := types.GetMeta(out)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.VersionID != "7" {
		t.Errorf("VersionID = %q, want 7", meta.VersionID)
	}
	if meta.LastUpdated.IsZero() {
		t.Error("LastUpdated should be preserved")
	}

	refsIn, err := types.GetReferences(data)
	if err != nil {
		t.Fatalf("GetReferences input: %v", err)
	}
	refsOut, err := types.GetReferences(out)
	if err != nil {
		t.Fatalf("GetReferences output: %v", err)
	}
	if len(refsOut) != len(refsIn) {
		t.Fatalf("reference count = %d, want %d", len(refsOut), len(refsIn))
	}
	if !referencesEqual(refsIn, refsOut) {
		t.Errorf("references differ:\nin:  %+v\nout: %+v", refsIn, refsOut)
	}

	envelope, err := codec.ProtoToEnvelope("Observation", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	if envelope.ID != "obs-1" {
		t.Errorf("ID = %q, want obs-1", envelope.ID)
	}
	if envelope.VersionID != "7" {
		t.Errorf("VersionID = %q, want 7", envelope.VersionID)
	}
}

func TestIsProtoResource(t *testing.T) {
	if proto.IsProtoResource(nil) {
		t.Error("nil should not be a proto resource")
	}
	if proto.IsProtoResource("Patient") {
		t.Error("string should not be a proto resource")
	}

	codec := newCodec()
	pb, err := codec.ParseJSONToProto("", []byte(`{"resourceType":"Patient","id":"1"}`))
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	if !proto.IsProtoResource(pb) {
		t.Error("parsed contained resource should be recognized")
	}
}

func TestResourceTypeOfProto_Errors(t *testing.T) {
	if _, err := proto.ResourceTypeOfProto(nil); err == nil {
		t.Fatal("expected error for nil proto")
	}
	if _, err := proto.ResourceTypeOfProto("bad"); err == nil {
		t.Fatal("expected error for non-proto value")
	}
}

func referencesEqual(a, b []types.Reference) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, ref := range a {
		counts[referenceKey(ref)]++
	}
	for _, ref := range b {
		key := referenceKey(ref)
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func referenceKey(ref types.Reference) string {
	return ref.ResourceType + "\x00" + ref.ID + "\x00" + ref.Raw
}

func jsonSemanticallyEqual(a, b []byte) bool {
	var objA, objB map[string]interface{}
	if err := json.Unmarshal(a, &objA); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &objB); err != nil {
		return false
	}
	normA, err := types.NormalizeJSON(mustMarshal(objA))
	if err != nil {
		return false
	}
	normB, err := types.NormalizeJSON(mustMarshal(objB))
	if err != nil {
		return false
	}
	return string(normA) == string(normB)
}

func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
