package types_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/types"
)

func TestJSONCodec_ParsePatient(t *testing.T) {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"meta": {
			"versionId": "2",
			"lastUpdated": "2017-01-01T00:00:00.000+00:00"
		},
		"name": [{"text": "Jane"}]
	}`)

	codec := types.NewJSONCodec()
	envelope, err := codec.ParseJSON("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
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
	if envelope.Proto != nil {
		t.Error("Proto should be nil in MVP")
	}
	if envelope.Hash == "" {
		t.Error("Hash should be set")
	}
	if !json.Valid(envelope.JSON) {
		t.Error("JSON should be valid")
	}
}

func TestJSONCodec_ParseObservation(t *testing.T) {
	data := []byte(`{"resourceType":"Observation","id":"obs-1","status":"final"}`)
	codec := types.NewJSONCodec()
	envelope, err := codec.ParseJSON("", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if envelope.ResourceType != "Observation" {
		t.Errorf("ResourceType = %q, want Observation", envelope.ResourceType)
	}
	if envelope.ID != "obs-1" {
		t.Errorf("ID = %q, want obs-1", envelope.ID)
	}
}

func TestJSONCodec_RejectInvalidJSON(t *testing.T) {
	codec := types.NewJSONCodec()
	_, err := codec.ParseJSON("", []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONCodec_RejectMissingResourceType(t *testing.T) {
	codec := types.NewJSONCodec()
	_, err := codec.ParseJSON("", []byte(`{"id":"x"}`))
	if err == nil {
		t.Fatal("expected error for missing resourceType")
	}
}

func TestJSONCodec_RejectMismatchedResourceType(t *testing.T) {
	codec := types.NewJSONCodec()
	_, err := codec.ParseJSON("Patient", []byte(`{"resourceType":"Observation","id":"1"}`))
	if err == nil {
		t.Fatal("expected error for resourceType mismatch")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error = %v, want mismatch", err)
	}
}

func TestJSONCodec_ToJSON(t *testing.T) {
	codec := types.NewJSONCodec()
	data := []byte(`{"resourceType":"Patient","id":"1"}`)
	envelope, err := codec.ParseJSON("", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	out, err := codec.ToJSON(envelope)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if string(out) != string(envelope.JSON) {
		t.Errorf("ToJSON = %s, want %s", out, envelope.JSON)
	}
}

func TestJSONCodec_ToJSONErrors(t *testing.T) {
	codec := types.NewJSONCodec()
	_, err := codec.ToJSON(nil)
	if err == nil {
		t.Fatal("expected error for nil resource")
	}
	_, err = codec.ToJSON(&types.ResourceEnvelope{})
	if err == nil {
		t.Fatal("expected error for empty JSON")
	}
}

func TestNormalizeJSON_EquivalentPayloads(t *testing.T) {
	a := []byte(`{"resourceType":"Patient","id":"1","name":[{"text":"A"}]}`)
	b := []byte(`{
		"id": "1",
		"name": [
			{"text": "A"}
		],
		"resourceType": "Patient"
	}`)

	normA, err := types.NormalizeJSON(a)
	if err != nil {
		t.Fatalf("NormalizeJSON a: %v", err)
	}
	normB, err := types.NormalizeJSON(b)
	if err != nil {
		t.Fatalf("NormalizeJSON b: %v", err)
	}
	if string(normA) != string(normB) {
		t.Errorf("normalized payloads differ:\na: %s\nb: %s", normA, normB)
	}
}

func TestHashResource_EquivalentAndChanged(t *testing.T) {
	a := []byte(`{"resourceType":"Patient","id":"1"}`)
	b := []byte(`{ "id" : "1" , "resourceType" : "Patient" }`)
	c := []byte(`{"resourceType":"Patient","id":"2"}`)

	hashA, err := types.HashResource(a)
	if err != nil {
		t.Fatalf("HashResource a: %v", err)
	}
	hashB, err := types.HashResource(b)
	if err != nil {
		t.Fatalf("HashResource b: %v", err)
	}
	hashC, err := types.HashResource(c)
	if err != nil {
		t.Fatalf("HashResource c: %v", err)
	}
	if hashA != hashB {
		t.Errorf("equivalent hashes differ: %s vs %s", hashA, hashB)
	}
	if hashA == hashC {
		t.Errorf("different content produced same hash: %s", hashA)
	}
}

func TestGetAndSetID(t *testing.T) {
	base := []byte(`{"resourceType":"Patient","id":"old","active":true}`)

	id, err := types.GetID(base)
	if err != nil {
		t.Fatalf("GetID: %v", err)
	}
	if id != "old" {
		t.Errorf("GetID = %q, want old", id)
	}

	updated, err := types.SetID(base, "new")
	if err != nil {
		t.Fatalf("SetID: %v", err)
	}
	id, err = types.GetID(updated)
	if err != nil {
		t.Fatalf("GetID after SetID: %v", err)
	}
	if id != "new" {
		t.Errorf("id after SetID = %q, want new", id)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(updated, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if active, ok := obj["active"].(bool); !ok || !active {
		t.Error("active field should be preserved")
	}
}

func TestGetAndSetMeta(t *testing.T) {
	base := []byte(`{"resourceType":"Patient","id":"1","gender":"female"}`)
	ts := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)

	meta, err := types.GetMeta(base)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if !meta.LastUpdated.IsZero() || meta.VersionID != "" {
		t.Errorf("expected zero meta, got %+v", meta)
	}

	updated, err := types.SetMeta(base, types.Meta{
		VersionID:   "3",
		LastUpdated: ts,
	})
	if err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	meta, err = types.GetMeta(updated)
	if err != nil {
		t.Fatalf("GetMeta after SetMeta: %v", err)
	}
	if meta.VersionID != "3" {
		t.Errorf("VersionID = %q, want 3", meta.VersionID)
	}
	if !meta.LastUpdated.Equal(ts) {
		t.Errorf("LastUpdated = %v, want %v", meta.LastUpdated, ts)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(updated, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gender, ok := obj["gender"].(string); !ok || gender != "female" {
		t.Error("gender field should be preserved")
	}
}

func TestGetReferences(t *testing.T) {
	data := []byte(`{
		"resourceType": "Observation",
		"subject": {"reference": "Patient/123"},
		"performer": [
			{"reference": "Practitioner/abc"},
			{"reference": "urn:uuid:550e8400-e29b-41d4-a716-446655440000"}
		],
		"basedOn": [{"reference": "http://example.com/fhir/CarePlan/99"}],
		"component": [{
			"code": {"text": "nested"},
			"valueReference": {"reference": "Device/def"}
		}]
	}`)

	refs, err := types.GetReferences(data)
	if err != nil {
		t.Fatalf("GetReferences: %v", err)
	}
	if len(refs) != 5 {
		t.Fatalf("got %d references, want 5", len(refs))
	}

	byRaw := make(map[string]types.Reference, len(refs))
	for _, ref := range refs {
		byRaw[ref.Raw] = ref
	}

	patient := byRaw["Patient/123"]
	if patient.Raw != "Patient/123" || patient.ResourceType != "Patient" || patient.ID != "123" {
		t.Errorf("Patient/123 = %+v", patient)
	}
	practitioner := byRaw["Practitioner/abc"]
	if practitioner.ResourceType != "Practitioner" || practitioner.ID != "abc" {
		t.Errorf("Practitioner/abc = %+v", practitioner)
	}
	urn := byRaw["urn:uuid:550e8400-e29b-41d4-a716-446655440000"]
	if urn.ResourceType != "" || urn.ID != "" {
		t.Errorf("URN typed fields should be empty: %+v", urn)
	}
	url := byRaw["http://example.com/fhir/CarePlan/99"]
	if url.ResourceType != "" || url.ID != "" {
		t.Errorf("URL typed fields should be empty: %+v", url)
	}
	device := byRaw["Device/def"]
	if device.ResourceType != "Device" || device.ID != "def" {
		t.Errorf("Device/def = %+v", device)
	}
}

func TestGetReferences_UntypedID(t *testing.T) {
	data := []byte(`{"resourceType":"Provenance","target":[{"reference":"only-an-id"}]}`)
	refs, err := types.GetReferences(data)
	if err != nil {
		t.Fatalf("GetReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d references, want 1", len(refs))
	}
	if refs[0].Raw != "only-an-id" {
		t.Errorf("Raw = %q, want only-an-id", refs[0].Raw)
	}
	if refs[0].ResourceType != "" || refs[0].ID != "" {
		t.Errorf("typed fields should be empty: %+v", refs[0])
	}
}

func TestGetResourceType(t *testing.T) {
	rt, err := types.GetResourceType([]byte(`{"resourceType":"Patient"}`))
	if err != nil {
		t.Fatalf("GetResourceType: %v", err)
	}
	if rt != "Patient" {
		t.Errorf("got %q, want Patient", rt)
	}
}

func TestOperationOutcome_MarshalUnmarshal(t *testing.T) {
	original := types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{
			{
				Severity:    "error",
				Code:        "invalid",
				Diagnostics: "bad field",
				Expression:  []string{"Patient.name"},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded types.OperationOutcome
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ResourceType != "OperationOutcome" {
		t.Errorf("ResourceType = %q", decoded.ResourceType)
	}
	if len(decoded.Issue) != 1 {
		t.Fatalf("Issue len = %d, want 1", len(decoded.Issue))
	}
	if decoded.Issue[0].Severity != "error" {
		t.Errorf("Severity = %q, want error", decoded.Issue[0].Severity)
	}
	if decoded.Issue[0].Code != "invalid" {
		t.Errorf("Code = %q, want invalid", decoded.Issue[0].Code)
	}
	if decoded.Issue[0].Diagnostics != "bad field" {
		t.Errorf("Diagnostics = %q", decoded.Issue[0].Diagnostics)
	}
	if len(decoded.Issue[0].Expression) != 1 || decoded.Issue[0].Expression[0] != "Patient.name" {
		t.Errorf("Expression = %v", decoded.Issue[0].Expression)
	}
}
