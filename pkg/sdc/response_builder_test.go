package sdc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseBuilderSetsCanonicalQuestionnaireAndValueTypes(t *testing.T) {
	q := Version(NewDraft("http://example/q", []Item{
		{LinkID: "name", Type: "string", Required: true},
		{LinkID: "active", Type: "boolean"},
		{LinkID: "score", Type: "integer"},
	}), "1")

	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	r, err := builder.
		Set("name", "Ada").
		Set("active", true).
		Set("score", 42).
		Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Questionnaire != "http://example/q|1" {
		t.Fatalf("questionnaire=%q", r.Questionnaire)
	}
	if r.Status != "in-progress" {
		t.Fatalf("status=%q", r.Status)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(b)
	for _, want := range []string{`"valueString":"Ada"`, `"valueBoolean":true`, `"valueInteger":42`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("missing %s in %s", want, payload)
		}
	}
}

func TestResponseBuilderConvertsChoiceCodesToValueCoding(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "color",
		Type:   "choice",
		AnswerOption: []AnswerOption{{
			Value: Coding{System: "http://example/colors", Code: "red", Display: "Red"},
		}},
	}})

	r, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := r.SetCoding("color", "red").Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	answer := response.Item[0].Answer[0]
	coding, ok := answer.Value.(Coding)
	if !ok || coding.Code != "red" || coding.System != "http://example/colors" {
		t.Fatalf("coding: %#v", answer.Value)
	}
	b, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"valueCoding"`) {
		t.Fatalf("expected valueCoding, got %s", b)
	}
}

func TestResponseBuilderPreservesFormedCodingWithoutChangingMeaning(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "color",
		Type:   "choice",
		AnswerOption: []AnswerOption{{
			Value: Coding{System: "http://example/colors", Code: "red", Display: "Red"},
		}},
	}})
	provided := Coding{System: "http://other", Code: "blue", Display: "Blue"}

	r, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	r.Set("color", provided)
	preview := r.Preview()
	if len(preview.Item) != 1 || !Equal(preview.Item[0].Answer[0].Value, provided) {
		t.Fatalf("builder changed formed coding: %#v", preview.Item[0].Answer[0].Value)
	}
	_, err = r.Build(ValidationOptions{})
	if err == nil {
		t.Fatal("expected validation failure for out-of-option coding")
	}
	outcome, ok := OutcomeFromError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(outcome.Issue) == 0 || outcome.Issue[0].Code != "code-invalid" {
		t.Fatalf("outcome: %#v", outcome)
	}
}

func TestResponseBuilderHandlesNestedAndRepeatedItems(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:  "group",
		Type:    "group",
		Repeats: true,
		Item: []Item{
			{LinkID: "name", Type: "string"},
			{LinkID: "tags", Type: "string", Repeats: true},
		},
	}})

	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := builder.
		SetAt(ItemPath{{LinkID: "group", Index: 0}}, "name", "Ada").
		AppendAnswerAt(ItemPath{{LinkID: "group", Index: 0}}, "tags", "a").
		AppendAnswerAt(ItemPath{{LinkID: "group", Index: 0}}, "tags", "b").
		SetAt(ItemPath{{LinkID: "group", Index: 1}}, "name", "Grace").
		Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Item) != 2 {
		t.Fatalf("groups=%d", len(response.Item))
	}
	if response.Item[0].Item[0].Answer[0].Value != "Ada" {
		t.Fatalf("first group name: %#v", response.Item[0].Item)
	}
	if len(response.Item[0].Item[1].Answer) != 2 {
		t.Fatalf("tags: %#v", response.Item[0].Item[1].Answer)
	}
	if response.Item[1].Item[0].Answer[0].Value != "Grace" {
		t.Fatalf("second group name: %#v", response.Item[1].Item)
	}
}

func TestResponseBuilderAutoPlacesUniqueNestedLinkIDs(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "group",
		Type:   "group",
		Item:   []Item{{LinkID: "nested", Type: "string", Required: true}},
	}})

	response, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	r, err := response.Set("nested", "ok").Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Item) != 1 || len(r.Item[0].Item) != 1 || r.Item[0].Item[0].Answer[0].Value != "ok" {
		t.Fatalf("nested placement failed: %#v", r.Item)
	}
}

func TestResponseBuilderReportsUnknownFieldsAndInvalidValues(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "age", Type: "integer"}})
	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	_, err = builder.
		Set("missing", 1).
		Set("age", "not-a-number").
		Build(ValidationOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	outcome, ok := OutcomeFromError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	codes := map[string]bool{}
	for _, issue := range outcome.Issue {
		codes[issue.Code] = true
	}
	if !codes["not-found"] || !codes["type"] {
		t.Fatalf("issues: %#v", outcome.Issue)
	}
}

func TestResponseBuilderReportsAmbiguousLinkIDs(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "shared", Type: "string"},
		{LinkID: "group", Type: "group", Item: []Item{{LinkID: "shared", Type: "string"}}},
	})
	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	_, err = builder.Set("shared", "x").Build(ValidationOptions{})
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	outcome, ok := OutcomeFromError(err)
	if !ok || len(outcome.Issue) == 0 || outcome.Issue[0].Code != "duplicate" {
		t.Fatalf("outcome: %#v", outcome)
	}
}

func TestResponseBuilderBuildResourceReturnsEnvelope(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "x", Type: "string"}})
	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	env, err := builder.Set("x", "ok").BuildResource(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if env.ResourceType != "QuestionnaireResponse" {
		t.Fatalf("resource type=%q", env.ResourceType)
	}
}

func TestResponseBuilderOpenChoiceAcceptsFreeText(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "color",
		Type:   "open-choice",
		AnswerOption: []AnswerOption{{
			Value: Coding{Code: "red", Display: "Red"},
		}},
	}})
	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := builder.Set("color", "custom").Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Item[0].Answer[0].Value != "custom" {
		t.Fatalf("value: %#v", response.Item[0].Answer[0].Value)
	}
}

func TestResponseBuilderSupportsItemControlledNesting(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "trigger",
		Type:   "choice",
		AnswerOption: []AnswerOption{{
			Value: Coding{Code: "yes", Display: "Yes"},
		}},
		Item: []Item{{LinkID: "detail", Type: "string", Required: true}},
	}})

	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := builder.
		SetCoding("trigger", "yes").
		Set("detail", "extra").
		Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Item) != 1 || len(response.Item[0].Answer) != 1 {
		t.Fatalf("trigger response: %#v", response.Item)
	}
	if len(response.Item[0].Answer[0].Item) != 1 || response.Item[0].Answer[0].Item[0].Answer[0].Value != "extra" {
		t.Fatalf("item-controlled nesting failed: %#v", response.Item[0].Answer[0].Item)
	}

	builder, err = NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err = builder.
		SetCoding("trigger", "yes").
		SetAtAnswer(ItemPath{{LinkID: "trigger"}}, 0, "detail", "explicit").
		Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Item[0].Answer[0].Item[0].Answer[0].Value != "explicit" {
		t.Fatalf("SetAtAnswer failed: %#v", response.Item[0].Answer[0].Item)
	}
}

func TestResponseBuilderInGroupSelectsRepeatingInstance(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:  "group",
		Type:    "group",
		Repeats: true,
		Item:    []Item{{LinkID: "name", Type: "string"}},
	}})

	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := builder.
		InGroup("group", 1).
		Set("name", "Grace").
		Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Item) != 2 {
		t.Fatalf("groups=%d", len(response.Item))
	}
	if response.Item[1].Item[0].Answer[0].Value != "Grace" {
		t.Fatalf("second group name: %#v", response.Item)
	}
}

func TestResponseBuilderMatchesChoiceBySystemAndCode(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "status",
		Type:   "choice",
		AnswerOption: []AnswerOption{
			{Value: Coding{System: "http://a", Code: "active"}},
			{Value: Coding{System: "http://b", Code: "active"}},
		},
	}})

	builder, err := NewResponse(q)
	if err != nil {
		t.Fatal(err)
	}
	response, err := builder.SetCodingWithSystem("status", "http://b", "active").Build(ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	coding := response.Item[0].Answer[0].Value.(Coding)
	if coding.System != "http://b" {
		t.Fatalf("coding: %#v", coding)
	}
}
