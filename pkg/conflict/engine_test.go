package conflict_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conflict"
)

func TestClassificationStaleBaseOnly(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"birthDate": "2000-01-01",
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"birthDate": "2000-02-02",
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"birthDate": "2000-01-01",
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if res.Classification != conflict.ClassificationStaleBaseOnly {
		t.Fatalf("classification = %q", res.Classification)
	}
	if res.AutoMergeable {
		t.Fatalf("expected no auto-merge for birthDate change")
	}
}

func TestClassificationSafeNonOverlappingPatient(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"address": []any{map[string]any{"city": "NYC"}},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if res.Classification != conflict.ClassificationSameResourceNonOverlap {
		t.Fatalf("classification = %q", res.Classification)
	}
	if res.Risk != conflict.RiskLevelSafe {
		t.Fatalf("risk = %q", res.Risk)
	}
	if !res.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
}

func TestClassificationRequiresExplicitPolicyForBothSides(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"gender": "female",
	})

	result := conflict.NewDefaultEngine().Detect(
		localUpdate("Patient", "p1", "v1", "v2", local),
		base,
		current,
	)

	if result.AutoMergeable {
		t.Fatal("expected an unruled remote field to require review")
	}
	if result.Risk != conflict.RiskLevelReview {
		t.Fatalf("risk = %q, want review", result.Risk)
	}
}

func TestClassificationClinicalHotSpot(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"birthDate": "2000-01-01",
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"birthDate": "2000-02-02",
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if res.Risk != conflict.RiskLevelReview {
		t.Fatalf("risk = %q", res.Risk)
	}
	if res.AutoMergeable {
		t.Fatalf("expected no auto-merge for birthDate conflict")
	}
}

func TestClassificationUnsupportedOverlap(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"name": []any{map[string]any{"family": "Smith"}},
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"name": []any{map[string]any{"family": "Jones"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"name": []any{map[string]any{"family": "Taylor"}},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if res.AutoMergeable {
		t.Fatalf("expected no auto-merge for unsupported name overlap")
	}
	if res.Risk != conflict.RiskLevelReview {
		t.Fatalf("risk = %q", res.Risk)
	}
}

func TestMergePatientTelecomAuto(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"address": []any{map[string]any{"city": "NYC"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if !mergeRes.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
	assertMergedField(t, mergeRes.Merged.JSON, "telecom", []any{map[string]any{"system": "phone", "value": "111"}})
	assertMergedField(t, mergeRes.Merged.JSON, "address", []any{map[string]any{"city": "NYC"}})
	assertPatchContains(t, mergeRes.Patch, "add", "/telecom/-")
}

func TestMergePatientAddressAuto(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"address": []any{map[string]any{"city": "LA"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if !mergeRes.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
	assertMergedField(t, mergeRes.Merged.JSON, "address", []any{map[string]any{"city": "LA"}})
	assertMergedField(t, mergeRes.Merged.JSON, "telecom", []any{map[string]any{"system": "phone", "value": "111"}})
	assertPatchContains(t, mergeRes.Patch, "add", "/address/-")
}

func TestMergeAppointmentNoteAppend(t *testing.T) {
	base := resourceEnvelope(t, "Appointment", "a1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Appointment", "a1", "v2", map[string]any{
		"note": []any{map[string]any{"text": "local note"}},
	})
	current := resourceEnvelope(t, "Appointment", "a1", "v3", map[string]any{
		"note": []any{map[string]any{"text": "remote note"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Appointment", "a1", "v1", "v2", local), base, current)

	if !mergeRes.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
	if mergeRes.Result.Classification != conflict.ClassificationAppendOnlyCompatible {
		t.Fatalf("classification = %q", mergeRes.Result.Classification)
	}
	assertMergedField(t, mergeRes.Merged.JSON, "note", []any{
		map[string]any{"text": "remote note"},
		map[string]any{"text": "local note"},
	})
	assertPatchContains(t, mergeRes.Patch, "add", "/note/-")
}

func TestMergeEncounterStatusHistoryAppend(t *testing.T) {
	base := resourceEnvelope(t, "Encounter", "e1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Encounter", "e1", "v2", map[string]any{
		"statusHistory": []any{map[string]any{"status": "in-progress"}},
	})
	current := resourceEnvelope(t, "Encounter", "e1", "v3", map[string]any{
		"statusHistory": []any{map[string]any{"status": "finished"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Encounter", "e1", "v1", "v2", local), base, current)

	if !mergeRes.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
	assertMergedField(t, mergeRes.Merged.JSON, "statusHistory", []any{
		map[string]any{"status": "finished"},
		map[string]any{"status": "in-progress"},
	})
}

func TestMergeObservationValueReview(t *testing.T) {
	base := resourceEnvelope(t, "Observation", "o1", "v1", map[string]any{
		"valueQuantity": map[string]any{"value": 10.0},
	})
	local := resourceEnvelope(t, "Observation", "o1", "v2", map[string]any{
		"valueQuantity": map[string]any{"value": 20.0},
	})
	current := resourceEnvelope(t, "Observation", "o1", "v3", map[string]any{
		"status": "final",
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Observation", "o1", "v1", "v2", local), base, current)

	if mergeRes.AutoMergeable {
		t.Fatalf("expected review for Observation.value")
	}
	if mergeRes.Result.Risk != conflict.RiskLevelReview {
		t.Fatalf("risk = %q", mergeRes.Result.Risk)
	}
	if mergeRes.Review.Reason == "" {
		t.Fatalf("expected review reason")
	}
}

func TestMergePatientBirthDateReview(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"birthDate": "2000-01-01",
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"birthDate": "2000-02-02",
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if mergeRes.AutoMergeable {
		t.Fatalf("expected review for birthDate")
	}
	if mergeRes.Result.Risk != conflict.RiskLevelReview {
		t.Fatalf("risk = %q", mergeRes.Result.Risk)
	}
}

func TestPatchRebasesFromCurrent(t *testing.T) {
	base := resourceEnvelope(t, "Appointment", "a1", "v1", map[string]any{})
	local := resourceEnvelope(t, "Appointment", "a1", "v2", map[string]any{
		"note": []any{map[string]any{"text": "local"}},
	})
	current := resourceEnvelope(t, "Appointment", "a1", "v3", map[string]any{
		"note": []any{map[string]any{"text": "remote"}},
	})

	eng := conflict.NewDefaultEngine()
	mergeRes := eng.Merge(localUpdate("Appointment", "a1", "v1", "v2", local), base, current)

	if !mergeRes.AutoMergeable {
		t.Fatalf("expected auto-merge")
	}
	applied := applyPatch(t, current.JSON, mergeRes.Patch)
	assertJSONField(t, applied, "note", []any{
		map[string]any{"text": "remote"},
		map[string]any{"text": "local"},
	})
}

func assertMergedField(t *testing.T, mergedJSON []byte, field string, want any) {
	t.Helper()
	assertJSONField(t, mergedJSON, field, want)
}

func assertJSONField(t *testing.T, jsonData []byte, field string, want any) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(jsonData, &obj); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	got := obj[field]
	if !jsonEqual(got, want) {
		t.Fatalf("field %s = %+v, want %+v", field, got, want)
	}
}

func assertPatchContains(t *testing.T, patch []byte, op, path string) {
	t.Helper()
	var ops []conflict.Operation
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	for _, o := range ops {
		if o.Op == op && o.Path == path {
			return
		}
	}
	t.Fatalf("patch missing %s %s in %s", op, path, string(patch))
}

func applyPatch(t *testing.T, doc []byte, patch []byte) []byte {
	t.Helper()
	var obj any
	if err := json.Unmarshal(doc, &obj); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	var ops []conflict.Operation
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	for _, op := range ops {
		if op.Op != "add" && op.Op != "replace" {
			t.Fatalf("unsupported op %s", op.Op)
		}
		var err error
		obj, err = applyAtPath(obj, op.Path, op.Value, op.Op)
		if err != nil {
			t.Fatalf("apply patch: %v", err)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal patched: %v", err)
	}
	return out
}

func applyAtPath(v any, path string, value any, op string) (any, error) {
	if path == "" {
		return value, nil
	}
	segments := strings.Split(path, "/")[1:]
	return applySegments(v, segments, value, op)
}

func applySegments(v any, segments []string, value any, op string) (any, error) {
	if len(segments) == 0 {
		return value, nil
	}
	seg := segments[0]
	rest := segments[1:]
	if len(rest) == 0 && seg == "-" {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("'-' requires an array at %s", strings.Join(segments, "/"))
		}
		return append(arr, value), nil
	}
	if len(rest) == 0 {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("replace requires an object at %s", strings.Join(segments, "/"))
		}
		m[seg] = value
		return m, nil
	}
	switch m := v.(type) {
	case map[string]any:
		child := m[seg]
		newChild, err := applySegments(child, rest, value, op)
		if err != nil {
			return nil, err
		}
		m[seg] = newChild
		return m, nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= len(m) {
			return nil, fmt.Errorf("index out of range: %d", idx)
		}
		child := m[idx]
		newChild, err := applySegments(child, rest, value, op)
		if err != nil {
			return nil, err
		}
		m[idx] = newChild
		return m, nil
	default:
		return nil, fmt.Errorf("cannot traverse %s", seg)
	}
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
