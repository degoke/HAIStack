package validate

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type corruptProfileCatalog struct {
	raw map[string][]byte
}

func (c corruptProfileCatalog) GetStructureDefinition(canonicalURL string) (*StructureDefinition, bool) {
	sd, err := c.ResolveStructureDefinition(canonicalURL)
	if err != nil {
		return nil, false
	}
	return sd, true
}

func (c corruptProfileCatalog) ResolveStructureDefinition(canonicalURL string) (*StructureDefinition, error) {
	raw, ok := c.raw[canonicalURL]
	if !ok {
		return nil, ErrProfileNotFound
	}
	sd, ok, err := parseStructureDefinition(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrProfileNotFound
	}
	return sd, nil
}

func TestLookupStructureDefinitionParseError(t *testing.T) {
	catalog := corruptProfileCatalog{
		raw: map[string][]byte{
			"http://example.org/bad": []byte(`{"resourceType":"StructureDefinition","url":"http://example.org/bad","type":true}`),
		},
	}
	_, err := lookupStructureDefinition(catalog, "http://example.org/bad")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestValidateReportsProfileParseError(t *testing.T) {
	catalog := corruptProfileCatalog{
		raw: map[string][]byte{
			"http://example.org/bad": []byte(`{"resourceType":"StructureDefinition","url":"http://example.org/bad","type":true}`),
		},
	}
	eng, err := NewEngine(Config{ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"p1",
			"meta":{"profile":["http://example.org/bad"]},
			"name":[{"family":"Doe"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, ValidateOptions{
		EnforceDeclaredProfiles: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected invalid result")
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "profile-parse" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected profile-parse issue, got %+v", result.Issues)
	}
}

type errorFHIRPathEngine struct{}

func (errorFHIRPathEngine) Compile(expr string) (fhirpath.CompiledExpression, error) {
	return nil, errors.New("not implemented")
}

func (errorFHIRPathEngine) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	return nil, errors.New("eval failed")
}

func (errorFHIRPathEngine) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	return false, errors.New("eval failed")
}

func (errorFHIRPathEngine) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	return "", errors.New("eval failed")
}

func TestValidateProfileConstraintsReportsFHIRPathError(t *testing.T) {
	catalog, err := LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","constraint":[
				{"key":"dom-2","severity":"error","expression":"name.exists()","human":"need a name"}
			]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{ProfileCatalog: catalog, FHIRPath: errorFHIRPathEngine{}})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), env, ValidateOptions{
		EnforceBaseProfile: true,
		ProfileConstraints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "invariant-evaluation" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected invariant-evaluation warning, got %+v", result.Issues)
	}
}
