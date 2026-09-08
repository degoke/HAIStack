package validate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
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

func TestProfileObjectCardinalityCountsComplexTypes(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","type":[{"code":"HumanName"}]},
			{"path":"Patient.maritalStatus","min":1,"max":"1","type":[{"code":"CodeableConcept"}]}
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

	missing := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), missing, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Mode:               validate.ValidationModeFast,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected missing maritalStatus to fail required cardinality")
	}
	assertInvalidCode(t, result, err, "required")

	present := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p2","name":[{"family":"Doe"}],"maritalStatus":{"coding":[{"system":"urn:marital","code":"M"}]}}`),
	}
	result, err = eng.Validate(context.Background(), present, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Mode:               validate.ValidationModeFast,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected present maritalStatus object to satisfy min=1, got %+v", result.Issues)
	}
}

func TestValidationModeFullSkipsPreferredTerminologyBindings(t *testing.T) {
	ctx := context.Background()
	rawSD := []byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","type":[{"code":"HumanName"}]},
			{"path":"Patient.maritalStatus","min":0,"max":"1","type":[{"code":"CodeableConcept"}],
				"binding":{"strength":"preferred","valueSet":"urn:marital"}}
		]}
	}`)
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{rawSD})
	if err != nil {
		t.Fatal(err)
	}
	st := terminology.NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:marital","version":"1","concept":[{"code":"M"}]}`)
	if err := st.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "CodeSystem", CanonicalURL: "urn:marital", Version: "1", Status: "active", ResourceJSON: cs}); err != nil {
		t.Fatal(err)
	}
	if err := terminology.Compile(ctx, st, "s", "", cs); err != nil {
		t.Fatal(err)
	}
	vs := []byte(`{"resourceType":"ValueSet","url":"urn:marital","version":"1","status":"active","compose":{"include":[{"system":"urn:marital"}]}}`)
	if err := st.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "ValueSet", CanonicalURL: "urn:marital", Version: "1", Status: "active", ResourceJSON: vs}); err != nil {
		t.Fatal(err)
	}
	if err := terminology.Compile(ctx, st, "s", "", vs); err != nil {
		t.Fatal(err)
	}
	term := &terminology.LocalService{Store: st, ScopeID: "s"}
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
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","maritalStatus":{"coding":[{"system":"urn:marital","code":"bogus"}]},"name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(ctx, env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Terminology:        term,
		Mode:               validate.ValidationModeFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("preferred bindings should be skipped in full mode, got %+v", result.Issues)
	}
}

func TestProfileConstraintsCompileCache(t *testing.T) {
	constraint := `{"key":"dom-2","severity":"error","expression":"name.exists()","human":"need a name"}`
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","constraint":[` + constraint + `]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	var compileCount int
	var evalCount int
	fp := countingFHIRPath{compileCount: &compileCount, evals: &evalCount}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	opts := validate.ValidateOptions{
		EnforceBaseProfile: true,
		ProfileConstraints: true,
	}
	for i := 0; i < 2; i++ {
		if _, err := eng.Validate(context.Background(), env, opts); err != nil {
			t.Fatal(err)
		}
	}
	if compileCount != 1 {
		t.Fatalf("compile count = %d, want 1 (constraints cached on StructureDefinition)", compileCount)
	}
}

func TestProfileConstraintsReportsCompileFailure(t *testing.T) {
	constraint := `{"key":"dom-2","severity":"error","expression":"name.exists()","human":"need a name"}`
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","constraint":[` + constraint + `]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: compileErrorFHIRPath{}})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
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
		t.Fatalf("expected invariant-evaluation warning for compile failure, got %+v", result.Issues)
	}
}

type countingFHIRPath struct {
	evals        *int
	compileCount *int
}

func (c countingFHIRPath) Compile(expr string) (fhirpath.CompiledExpression, error) {
	if c.compileCount != nil {
		*c.compileCount++
	}
	return countingCompiled{evals: c.evals, expr: expr}, nil
}

func (c countingFHIRPath) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	return nil, nil
}

func (c countingFHIRPath) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	if c.evals != nil {
		*c.evals++
	}
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
	if c.evals != nil {
		*c.evals++
	}
	return true, nil
}

func (c countingCompiled) EvalString(ctx context.Context, resource any) (string, error) {
	return "", nil
}

type compileErrorFHIRPath struct{}

func (compileErrorFHIRPath) Compile(expr string) (fhirpath.CompiledExpression, error) {
	return nil, errors.New("compile failed")
}

func (compileErrorFHIRPath) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	return nil, nil
}

func (compileErrorFHIRPath) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	return true, nil
}

func (compileErrorFHIRPath) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	return "", nil
}
