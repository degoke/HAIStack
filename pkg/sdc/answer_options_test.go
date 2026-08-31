package sdc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustCodingOption(t *testing.T, code, display string, system ...string) AnswerOption {
	t.Helper()
	opt, err := CodingOption(code, display, system...)
	if err != nil {
		t.Fatal(err)
	}
	return opt
}

func mustStringOption(t *testing.T, s string) AnswerOption {
	t.Helper()
	opt, err := StringOption(s)
	if err != nil {
		t.Fatal(err)
	}
	return opt
}

func TestCodingOptionSerializesValueCoding(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "site",
		Type:   "choice",
		AnswerOption: []AnswerOption{
			mustCodingOption(t, "hospital", "Hospital", "http://example.org/sites"),
		},
	}})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(env.JSON)
	if !strings.Contains(raw, `"valueCoding"`) || strings.Contains(raw, `"valueString"`) {
		t.Fatalf("expected valueCoding in projection: %s", raw)
	}
	if !strings.Contains(raw, `"code":"hospital"`) || !strings.Contains(raw, `"display":"Hospital"`) {
		t.Fatalf("coding fields missing: %s", raw)
	}
	if !strings.Contains(raw, `"system":"http://example.org/sites"`) {
		t.Fatalf("coding system missing: %s", raw)
	}
}

func TestCodingOptionRejectsEmptyCodeAtConstruction(t *testing.T) {
	_, err := CodingOption("", "Display only")
	if err == nil {
		t.Fatal("expected construction error for empty code")
	}
	var optErr AnswerOptionValueError
	if !errors.As(err, &optErr) {
		t.Fatalf("expected AnswerOptionValueError, got %T: %v", err, err)
	}
	if optErr.ValueType != "Coding" || !strings.Contains(optErr.Error(), "code is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChoiceRejectsPlainStringAnswerOption(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:       "site",
		Type:         "choice",
		AnswerOption: []AnswerOption{{Value: "hospital"}},
	}})
	_, err := ProjectionEnvelope(q)
	if err == nil {
		t.Fatal("expected marshal error for plain string on choice item")
	}
	var optErr AnswerOptionValueError
	if !errors.As(err, &optErr) {
		t.Fatalf("expected AnswerOptionValueError, got %T: %v", err, err)
	}
	if optErr.ItemType != "choice" || optErr.LinkID != "site" {
		t.Fatalf("item context: linkId=%q type=%q", optErr.LinkID, optErr.ItemType)
	}
}

func TestChoiceRejectsMapCodingAnswerOption(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "unit",
		Type:   "choice",
		AnswerOption: []AnswerOption{{
			Value: map[string]any{"code": "kg", "display": "kg"},
		}},
	}})
	_, err := ProjectionEnvelope(q)
	if err == nil {
		t.Fatal("expected error for map[string]any coding value")
	}
	var optErr AnswerOptionValueError
	if !errors.As(err, &optErr) {
		t.Fatalf("expected AnswerOptionValueError, got %T: %v", err, err)
	}
	if optErr.LinkID != "unit" {
		t.Fatalf("linkId: got %q", optErr.LinkID)
	}
}

func TestOpenChoiceAcceptsStringAndCodingOptions(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "site",
		Type:   "open-choice",
		AnswerOption: []AnswerOption{
			mustCodingOption(t, "hospital", "Hospital"),
			mustStringOption(t, "other"),
		},
	}})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(env.JSON)
	if !strings.Contains(raw, `"valueCoding"`) || !strings.Contains(raw, `"valueString":"other"`) {
		t.Fatalf("expected both valueCoding and valueString: %s", raw)
	}
}

func TestRepeatedChoiceOptionsRoundTrip(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:  "roles",
		Type:    "choice",
		Repeats: true,
		AnswerOption: []AnswerOption{
			mustCodingOption(t, "admin", "Administrator"),
			mustCodingOption(t, "clinician", "Clinician", "http://example.org/roles"),
			mustCodingOption(t, "patient", "Patient"),
		},
	}})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := DecodeQuestionnaireResource(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(q2.Item[0].AnswerOption) != 3 {
		t.Fatalf("answer options: got %d", len(q2.Item[0].AnswerOption))
	}
	for i, opt := range q2.Item[0].AnswerOption {
		c, ok := answerOptionCoding(opt.Value)
		if !ok {
			t.Fatalf("option %d: expected Coding value, got %#v", i, opt.Value)
		}
		if c.Code == "" {
			t.Fatalf("option %d: empty code", i)
		}
	}
	env2, err := ProjectionEnvelope(q2)
	if err != nil {
		t.Fatal(err)
	}
	if env.Hash == "" || env.Hash != env2.Hash {
		t.Fatalf("hash changed across round trip: %q != %q", env.Hash, env2.Hash)
	}
	if _, err := ParseR4(env2); err != nil {
		t.Fatalf("R4 parse: %v\n%s", err, env2.JSON)
	}
}

func TestChoiceAnswerOptionDecodeEncodeRoundTrip(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"draft",
		"item":[{
			"linkId":"unit",
			"type":"choice",
			"answerOption":[
				{"valueCoding":{"system":"http://unitsofmeasure.org","code":"kg","display":"kilogram"},"initialSelected":true},
				{"valueCoding":{"code":"lb","display":"pound"}}
			]
		}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Item) != 1 || len(q.Item[0].AnswerOption) != 2 {
		t.Fatalf("decode: %#v", q.Item)
	}
	first, ok := answerOptionCoding(q.Item[0].AnswerOption[0].Value)
	if !ok || first.Code != "kg" || !q.Item[0].AnswerOption[0].InitialSelected {
		t.Fatalf("first option: %#v selected=%v", first, q.Item[0].AnswerOption[0].InitialSelected)
	}
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env.JSON), `"valueCoding"`) {
		t.Fatalf("re-projection lost valueCoding: %s", env.JSON)
	}
	outcome := ValidateQuestionnaire(q, ValidationOptions{})
	if HasErrors(outcome) {
		t.Fatalf("validation failed: %#v", outcome)
	}
}

func TestAnswerOptionMarshalRejectsInvalidValueCodingShape(t *testing.T) {
	opt := AnswerOption{Value: "hospital", ValueType: "Coding"}
	_, err := json.Marshal(opt)
	if err == nil {
		t.Fatal("expected marshal error for string under valueCoding")
	}
	var optErr AnswerOptionValueError
	if !errors.As(err, &optErr) {
		t.Fatalf("expected AnswerOptionValueError, got %T: %v", err, err)
	}
}

func TestChoiceRejectsDecodedValueStringOnReprojection(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"draft",
		"item":[{"linkId":"unit","type":"choice","answerOption":[{"valueString":"kg"}]}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ProjectionEnvelope(q)
	if err == nil {
		t.Fatal("expected error re-projecting valueString on choice item")
	}
	var optErr AnswerOptionValueError
	if !errors.As(err, &optErr) {
		t.Fatalf("expected AnswerOptionValueError, got %T: %v", err, err)
	}
	if optErr.LinkID != "unit" {
		t.Fatalf("linkId: got %q", optErr.LinkID)
	}
}
