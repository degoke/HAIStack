package validate_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/pkg/validate"
)

func newEngine(t *testing.T, cfg validate.Config) validate.Engine {
	t.Helper()
	eng, err := validate.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func defaultEngine(t *testing.T) validate.Engine {
	t.Helper()
	return newEngine(t, validate.Config{})
}

func parseEnvelope(t *testing.T, data []byte) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON("", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return env
}

func validPatientEnvelope(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return parseEnvelope(t, []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"name": [{"given": ["Jane"], "family": "Doe"}]
	}`))
}

func assertValid(t *testing.T, result *validate.ValidationResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if result == nil || !result.Valid {
		t.Fatalf("expected valid result, got %+v", result)
	}
}

func assertInvalidCode(t *testing.T, result *validate.ValidationResult, err error, code string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if result == nil || result.Valid {
		t.Fatal("expected invalid result")
	}
	for _, iss := range result.Issues {
		if iss.Code == code {
			return
		}
	}
	t.Fatalf("expected issue code %q, got %+v", code, result.Issues)
}

func TestValidPatientPasses(t *testing.T) {
	eng := defaultEngine(t)
	result, err := eng.Validate(context.Background(), validPatientEnvelope(t), validate.ValidateOptions{})
	assertValid(t, result, err)
}

func TestInvalidJSONFails(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{ResourceType: "Patient", JSON: []byte(`not json`)}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "invalid-json")
}

func TestMissingResourceTypeFails(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{JSON: []byte(`{"id":"x"}`)}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "missing-resource-type")
}

func TestUnknownResourceTypeFails(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "NotARealType",
		JSON:         []byte(`{"resourceType":"NotARealType","id":"x"}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "unknown-resource-type")
}

func TestInstalledTypeAllowlistRejectsNonInstalled(t *testing.T) {
	eng := newEngine(t, validate.Config{
		InstalledTypes: validate.MapResourceTypeRegistry{"Patient": {}},
	})
	env := &types.ResourceEnvelope{
		ResourceType: "Observation",
		JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","status":"final"}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "resource-type-not-installed")
}

func TestNoInstalledRegistryAllowsKnownTypes(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "Observation",
		JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"hr"}}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertValid(t, result, err)
}

func TestPerRequestInstalledRegistry(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "Observation",
		JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"hr"}}`),
	}
	opts := validate.ValidateOptions{
		ResourceTypeRegistry: validate.MapResourceTypeRegistry{"Patient": {}},
	}
	result, err := eng.Validate(context.Background(), env, opts)
	assertInvalidCode(t, result, err, "resource-type-not-installed")
}

func TestValidIDPasses(t *testing.T) {
	eng := defaultEngine(t)
	env := validPatientEnvelope(t)
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertValid(t, result, err)
}

func TestInvalidIDFails(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"bad id!"}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "invalid-id")
}

func TestRequireIDEnforcesPresence(t *testing.T) {
	eng := defaultEngine(t)
	env := parseEnvelope(t, []byte(`{"resourceType":"Patient","name":[{"family":"Doe"}]}`))
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{RequireID: true})
	assertInvalidCode(t, result, err, "missing-id")
}

func TestReferenceValidation(t *testing.T) {
	eng := defaultEngine(t)

	cases := []struct {
		name    string
		json    string
		valid   bool
		invalid bool
	}{
		{
			name:  "typed relative",
			json:  `{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"x"},"subject":{"reference":"Patient/123"}}`,
			valid: true,
		},
		{
			name:  "url",
			json:  `{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"x"},"basedOn":[{"reference":"http://example.com/fhir/CarePlan/99"}]}`,
			valid: true,
		},
		{
			name:  "urn",
			json:  `{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"x"},"performer":[{"reference":"urn:uuid:550e8400-e29b-41d4-a716-446655440000"}]}`,
			valid: true,
		},
		{
			name:  "fragment",
			json:  `{"resourceType":"Patient","id":"pat-1","link":[{"type":"seealso","other":{"reference":"#sibling"}}]}`,
			valid: true,
		},
		{
			name:  "untyped id",
			json:  `{"resourceType":"Provenance","id":"prov-1","target":[{"reference":"only-an-id"}],"recorded":"2020-01-01T00:00:00Z","agent":[{"who":{"reference":"Practitioner/abc"}}]}`,
			valid: true,
		},
		{
			name:    "broken typed relative",
			json:    `{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"x"},"subject":{"reference":"Patient/"}}`,
			invalid: true,
		},
		{
			name:    "empty reference",
			json:    `{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"x"},"subject":{"reference":""}}`,
			invalid: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := &types.ResourceEnvelope{JSON: []byte(tc.json)}
			result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if tc.valid && !result.Valid {
				t.Fatalf("expected valid, got %+v", result.Issues)
			}
			if tc.invalid {
				assertInvalidCode(t, result, err, "invalid-reference")
			}
		})
	}
}

func TestRequiredFieldChecks(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "Observation",
		JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","code":{"text":"hr"}}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "missing-required-field")
}

func TestProtoStructuralValidation(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1","active":"not-a-boolean"}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	assertInvalidCode(t, result, err, "structural")
}

func TestToOperationOutcomeMapsIssues(t *testing.T) {
	result := &validate.ValidationResult{
		Valid: false,
		Issues: []validate.ValidationIssue{
			{
				Severity:    "error",
				Code:        "invalid-id",
				Diagnostics: "id bad",
				Expression:  []string{"Resource.id"},
			},
			{
				Severity:    "error",
				Code:        "missing-required-field",
				Diagnostics: "status missing",
				Expression:  []string{"Observation.status"},
			},
		},
	}

	outcome := validate.ToOperationOutcome(result)
	if outcome == nil || outcome.ResourceType != "OperationOutcome" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.Issue) != 2 {
		t.Fatalf("issue count = %d, want 2", len(outcome.Issue))
	}
	if outcome.Issue[0].Code != "invalid-id" || outcome.Issue[0].Diagnostics != "id bad" {
		t.Fatalf("issue[0] = %+v", outcome.Issue[0])
	}
	if len(outcome.Issue[0].Expression) != 1 || outcome.Issue[0].Expression[0] != "Resource.id" {
		t.Fatalf("expression = %v", outcome.Issue[0].Expression)
	}

	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), "OperationOutcome") {
		t.Fatalf("json = %s", data)
	}
}

func TestNewCoreValidator(t *testing.T) {
	eng := defaultEngine(t)
	validator := validate.NewCoreValidator(eng, validate.ValidateOptions{})

	if err := validator.ValidateResource(context.Background(), validPatientEnvelope(t)); err != nil {
		t.Fatalf("valid patient: %v", err)
	}

	invalid := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"bad id!"}`),
	}
	if err := validator.ValidateResource(context.Background(), invalid); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestMissingResourceTypeCollectsOtherIssues(t *testing.T) {
	eng := defaultEngine(t)
	env := &types.ResourceEnvelope{JSON: []byte(`{"id":"bad id!"}`)}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{RequireID: true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result")
	}
	codes := issueCodes(result)
	if !codes["missing-resource-type"] {
		t.Fatalf("expected missing-resource-type, got %+v", result.Issues)
	}
	if !codes["invalid-id"] {
		t.Fatalf("expected invalid-id, got %+v", result.Issues)
	}
}

func TestInstalledTypeReportedWithOtherIssues(t *testing.T) {
	eng := newEngine(t, validate.Config{
		InstalledTypes: validate.MapResourceTypeRegistry{"Patient": {}},
	})
	env := &types.ResourceEnvelope{
		JSON: []byte(`{"resourceType":"Observation","id":"bad id!","status":"final","code":{"text":"hr"}}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	codes := issueCodes(result)
	if !codes["resource-type-not-installed"] {
		t.Fatalf("expected resource-type-not-installed, got %+v", result.Issues)
	}
	if !codes["invalid-id"] {
		t.Fatalf("expected invalid-id, got %+v", result.Issues)
	}
}

func issueCodes(result *validate.ValidationResult) map[string]bool {
	codes := make(map[string]bool, len(result.Issues))
	for _, iss := range result.Issues {
		codes[iss.Code] = true
	}
	return codes
}
