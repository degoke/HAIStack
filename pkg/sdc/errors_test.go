package sdc

import (
	"errors"
	"testing"
)

func TestOutcomeSummaryJoinsAllIssues(t *testing.T) {
	o := Outcome{
		ResourceType: "OperationOutcome",
		Issue: []Issue{
			{Severity: "error", Code: "required", Diagnostics: "required answer is missing"},
			{Severity: "error", Code: "type", Diagnostics: "answer type does not match question type"},
		},
	}
	got := OutcomeSummary(o)
	want := "required answer is missing; answer type does not match question type"
	if got != want {
		t.Fatalf("OutcomeSummary = %q, want %q", got, want)
	}
	if o.Error() != want {
		t.Fatalf("Outcome.Error = %q, want %q", o.Error(), want)
	}
}

func TestValidationErrorPreservesAllIssues(t *testing.T) {
	outcome := Outcome{
		ResourceType: "OperationOutcome",
		Issue: []Issue{
			{Severity: "error", Code: "required", Diagnostics: "first", Expression: []string{"item[a]"}, FieldPath: "item[a]"},
			{Severity: "error", Code: "type", Diagnostics: "second", Expression: []string{"item[b]"}, FieldPath: "item[b]"},
		},
	}
	err := ValidationError{Outcome: outcome}

	recovered, ok := OutcomeFromError(err)
	if !ok || len(recovered.Issue) != 2 {
		t.Fatalf("OutcomeFromError = %+v, ok=%v", recovered, ok)
	}

	fhirOutcome := err.OperationOutcome()
	if len(fhirOutcome.Issue) != 2 {
		t.Fatalf("OperationOutcome issue count = %d", len(fhirOutcome.Issue))
	}
	if fhirOutcome.Issue[0].Expression[0] != "item[a]" {
		t.Fatalf("expression not preserved: %#v", fhirOutcome.Issue[0])
	}

	wrapped := errors.Join(errors.New("outer"), err)
	recovered, ok = OutcomeFromError(wrapped)
	if !ok || len(recovered.Issue) != 2 {
		t.Fatalf("wrapped OutcomeFromError = %+v, ok=%v", recovered, ok)
	}

	cause := errors.New("underlying")
	wrappedVE := NewValidationError(outcome, cause)
	if !errors.Is(wrappedVE, cause) {
		t.Fatalf("Unwrap failed: %v", wrappedVE)
	}
}

func TestToOperationOutcomeUsesFieldPathWhenExpressionMissing(t *testing.T) {
	o := Outcome{
		Issue: []Issue{
			{Severity: "error", Code: "required", Diagnostics: "missing", FieldPath: "item[x]"},
		},
	}
	fhir := ToOperationOutcome(o)
	if len(fhir.Issue) != 1 || fhir.Issue[0].Expression[0] != "item[x]" {
		t.Fatalf("ToOperationOutcome = %#v", fhir)
	}
}

func TestBlankStringAnswerTreatedAsMissingForRequired(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "a", Type: "string", Required: true}})
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "a", Answer: []Answer{{Value: ""}}}},
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 1 || o.Issue[0].Code != "required" {
		t.Fatalf("blank required answer outcome: %#v", o)
	}
}

func TestBlankStringAnswerIgnoredForOptionalField(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "a", Type: "string"}})
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "a", Answer: []Answer{{Value: ""}}}},
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 0 {
		t.Fatalf("optional blank answer should not fail: %#v", o)
	}
}

func TestEnableWhenExistsTreatsBlankStringAsAbsent(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "trigger", Type: "string"},
		{LinkID: "dependent", Type: "string", EnableWhen: []EnableWhen{{Question: "trigger", Operator: "exists", Answer: false}}},
	})
	r := QuestionnaireResponse{
		Status: "in-progress",
		Item: []ResponseItem{
			{LinkID: "trigger", Answer: []Answer{{Value: ""}}},
			{LinkID: "dependent", Answer: []Answer{{Value: "visible"}}},
		},
	}
	if !Enabled(q.Item[1], r) {
		t.Fatal("dependent item should be enabled when trigger answer is blank")
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 0 {
		t.Fatalf("blank trigger should not invalidate dependent answer: %#v", o)
	}
}
