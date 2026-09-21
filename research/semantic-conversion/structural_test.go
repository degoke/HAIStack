package semanticconversion

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuralOKMatchesGoldMinusSource(t *testing.T) {
	pairs, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	var transformed int
	for _, pair := range pairs {
		got, _, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
		if err != nil {
			t.Fatalf("%s: convert: %v", pair.ID, err)
		}
		ok, msg, err := structuralOK(pair, got)
		if err != nil {
			t.Fatalf("%s: %v", pair.ID, err)
		}
		if !ok {
			t.Errorf("%s: %s", pair.ID, msg)
		}
		if pair.Spec != "" {
			transformed++
		}
	}
	if transformed == 0 {
		t.Fatal("expected transformed pairs")
	}
}

func TestStructuralOKRejectsDroppedCopyThrough(t *testing.T) {
	pairs, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		category string
		field    string
	}{
		{CategoryRenamed, "subject"},
		{CategoryRenamed, "id"},
		{CategoryCodeableConcept, "reason"}, // remapped from reasonCode/reasonReference
		{CategoryCodeableConcept, "subject"},
		{CategoryRemoved, "id"},
		{CategoryCardinality, "id"},
		{CategoryTypeChange, "subject"},
	}
	for _, tc := range cases {
		pair, ok := firstPair(pairs, tc.category)
		if !ok {
			t.Fatalf("missing %s pair", tc.category)
		}
		got, _, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
		if err != nil {
			t.Fatal(err)
		}
		dropped := dropField(t, got, tc.field)
		pass, _, err := structuralOK(pair, dropped)
		if err != nil {
			t.Fatal(err)
		}
		if pass {
			t.Errorf("%s: dropping %s must fail structural", pair.ID, tc.field)
		}
	}
}

func TestStructuralOKCodedMedicationMustKeepToyMed(t *testing.T) {
	pairs, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	pair, ok := firstPair(pairs, CategoryCodeableConcept)
	if !ok {
		t.Fatal("missing codeableconcept pair")
	}
	got, _, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
	if err != nil {
		t.Fatal(err)
	}
	stripped := stripMedicationSystem(t, got)
	pass, _, err := structuralOK(pair, stripped)
	if err != nil {
		t.Fatal(err)
	}
	if pass {
		t.Fatal("coded medication without toy-med system must fail structural")
	}
}

func TestStructuralOKTextOnlyMedicationDoesNotInventToyMed(t *testing.T) {
	pairs, err := LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	pair, ok := firstPair(pairs, CategoryTypeChange)
	if !ok {
		t.Fatal("missing type-change pair")
	}
	got, _, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
	if err != nil {
		t.Fatal(err)
	}
	pass, msg, err := structuralOK(pair, got)
	if err != nil {
		t.Fatal(err)
	}
	if !pass {
		t.Fatalf("text-only medication must match gold without toy-med: %s", msg)
	}
	if hasToyMed(got) {
		t.Fatal("Convert must not stamp toy-med on text-only medication")
	}
	invented := addToyMed(t, got)
	pass, _, err = structuralOK(pair, invented)
	if err != nil {
		t.Fatal(err)
	}
	if pass {
		t.Fatal("inventing toy-med on text-only medication must fail structural")
	}
}

func firstPair(pairs []Pair, category string) (Pair, bool) {
	for _, p := range pairs {
		if p.Category == category {
			return p, true
		}
	}
	return Pair{}, false
}

func dropField(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj[key]; !ok {
		t.Fatalf("converted payload missing %s to drop", key)
	}
	delete(obj, key)
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func stripMedicationSystem(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	meds, _ := obj["medication"].([]any)
	if len(meds) == 0 {
		t.Fatal("missing medication")
	}
	m, _ := meds[0].(map[string]any)
	concept, _ := m["concept"].(map[string]any)
	coding, _ := concept["coding"].([]any)
	if len(coding) == 0 {
		t.Fatal("expected coded medication")
	}
	cm, _ := coding[0].(map[string]any)
	delete(cm, "system")
	coding[0] = cm
	concept["coding"] = coding
	m["concept"] = concept
	meds[0] = m
	obj["medication"] = meds
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hasToyMed(raw json.RawMessage) bool {
	return strings.Contains(string(raw), MedicationToySystem)
}

func addToyMed(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	meds, _ := obj["medication"].([]any)
	m, _ := meds[0].(map[string]any)
	concept, _ := m["concept"].(map[string]any)
	concept["coding"] = []any{map[string]any{"system": MedicationToySystem, "code": "invented"}}
	m["concept"] = concept
	meds[0] = m
	obj["medication"] = meds
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEvalJSONPathFirstRequiresCollection(t *testing.T) {
	singleton := json.RawMessage(`{"resourceType":"Observation","interpretation":{"coding":[{"code":"N"}]}}`)
	err := assertJSONPath(singleton, "Observation.interpretation.first().coding.first().code", []string{"N"})
	if err == nil || !strings.Contains(err.Error(), "first() requires a collection") {
		t.Fatalf("first() on a singleton object must fail, got %v", err)
	}
	listed := json.RawMessage(`{"resourceType":"Observation","interpretation":[{"coding":[{"code":"N"}]}]}`)
	if err := assertJSONPath(listed, "Observation.interpretation.first().coding.first().code", []string{"N"}); err != nil {
		t.Fatalf("first() on a list: %v", err)
	}
}
