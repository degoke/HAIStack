package validate_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestValidationModeFastValidatesSlicing(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.identifier","min":0,"max":"*","type":[{"code":"Identifier"}],
				"slicing":{"discriminator":[{"type":"value","path":"system"}],"rules":"open"},
				"constraint":[]},
			{"path":"Patient.identifier:mrn","min":1,"max":"1","type":[{"code":"Identifier"}],
				"pattern":{"system":"urn:mrn"}}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Mode:               validate.ValidationModeFast,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected missing mrn slice to fail in fast mode")
	}
	assertInvalidCode(t, result, err, "required")
}

func TestProfileConstraintsSkipDuplicateInvariantKeys(t *testing.T) {
	sharedConstraint := `{"key":"dom-2","severity":"error","expression":"name.exists()","human":"need a name"}`
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{
		[]byte(`{
			"resourceType":"StructureDefinition",
			"url":"http://hl7.org/fhir/StructureDefinition/Patient",
			"type":"Patient",
			"kind":"resource",
			"snapshot":{"element":[
				{"path":"Patient","min":0,"max":"*"},
				{"path":"Patient.name","min":0,"max":"*","constraint":[` + sharedConstraint + `]}
			]}
		}`),
		[]byte(`{
			"resourceType":"StructureDefinition",
			"url":"http://example.org/Patient-copy",
			"type":"Patient",
			"kind":"resource",
			"snapshot":{"element":[
				{"path":"Patient","min":0,"max":"*"},
				{"path":"Patient.name","min":0,"max":"*","constraint":[` + sharedConstraint + `]}
			]}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var evalCount int
	fp := countingFHIRPath{evals: &evalCount}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","meta":{"profile":["http://example.org/Patient-copy"]},"name":[{"family":"Doe"}]}`),
	}
	_, err = eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		ProfileConstraints:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evalCount != 1 {
		t.Fatalf("eval count = %d, want 1 (duplicate dom-2 skipped)", evalCount)
	}
}

type countingFHIRPath struct {
	evals *int
}

func (c countingFHIRPath) Compile(expr string) (fhirpath.CompiledExpression, error) {
	return countingCompiled{evals: c.evals, expr: expr}, nil
}

func (c countingFHIRPath) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	return nil, nil
}

func (c countingFHIRPath) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	*c.evals++
	return true, nil
}

func (c countingFHIRPath) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	return "", nil
}

type countingCompiled struct {
	evals *int
	expr  string
}

func (c countingCompiled) Expr() string { return c.expr }

func (c countingCompiled) Eval(ctx context.Context, resource any) ([]fhirpath.Value, error) {
	return nil, nil
}

func (c countingCompiled) EvalBool(ctx context.Context, resource any) (bool, error) {
	*c.evals++
	return true, nil
}

func (c countingCompiled) EvalString(ctx context.Context, resource any) (string, error) {
	return "", nil
}
