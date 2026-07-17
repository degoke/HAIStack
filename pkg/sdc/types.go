package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const SDCPackageVersion = "3.0.0"
const SDCBaseURL = "http://hl7.org/fhir/uv/sdc/StructureDefinition/"

// ResourceService is the persistence surface needed by SDC. core.ResourceService satisfies it.
type ResourceService interface {
	Create(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Read(context.Context, string, string) (*types.ResourceEnvelope, error)
	Update(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
}

var _ ResourceService = (*core.ResourceService)(nil)

type DefinitionStore interface {
	Get(context.Context, string, string) (*store.DefinitionResourceRecord, error)
	List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error)
}

// Questionnaire is a JSON behavior projection only. It is not a persisted FHIR
// model; use DecodeQuestionnaireResource and ResourceEnvelope-based APIs at
// package boundaries. It remains exported temporarily for compatibility with
// the first SDC API revision.
type Questionnaire struct {
	ResourceType string         `json:"resourceType"`
	ID           string         `json:"id,omitempty"`
	URL          string         `json:"url,omitempty"`
	Version      string         `json:"version,omitempty"`
	Status       string         `json:"status,omitempty"`
	SubjectType  []string       `json:"subjectType,omitempty"`
	Item         []Item         `json:"item,omitempty"`
	Extension    []Extension    `json:"extension,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

// QuestionnaireResponse is a JSON behavior projection, not a replacement for
// pkg/proto/r4.QuestionnaireResponse or types.ResourceEnvelope. It remains
// exported temporarily for compatibility with the first SDC API revision.
type QuestionnaireResponse struct {
	ResourceType  string         `json:"resourceType"`
	ID            string         `json:"id,omitempty"`
	Questionnaire string         `json:"questionnaire,omitempty"`
	Status        string         `json:"status,omitempty"`
	Subject       map[string]any `json:"subject,omitempty"`
	Authored      string         `json:"authored,omitempty"`
	Item          []ResponseItem `json:"item,omitempty"`
}
type Item struct {
	LinkID                string         `json:"linkId"`
	Definition            string         `json:"definition,omitempty"`
	Code                  []Coding       `json:"code,omitempty"`
	Prefix                string         `json:"prefix,omitempty"`
	Text                  string         `json:"text,omitempty"`
	Type                  string         `json:"type"`
	EnableWhen            []EnableWhen   `json:"enableWhen,omitempty"`
	EnableBehavior        string         `json:"enableBehavior,omitempty"`
	EnableWhenExpression  *Expression    `json:"enableWhenExpression,omitempty"`
	Required              bool           `json:"required,omitempty"`
	Repeats               bool           `json:"repeats,omitempty"`
	ReadOnly              bool           `json:"readOnly,omitempty"`
	AnswerOption          []AnswerOption `json:"answerOption,omitempty"`
	AnswerValueSet        string         `json:"answerValueSet,omitempty"`
	Initial               []Answer       `json:"initial,omitempty"`
	Item                  []Item         `json:"item,omitempty"`
	Extension             []Extension    `json:"extension,omitempty"`
	AnswerExpression      *Expression    `json:"answerExpression,omitempty"`
	InitialExpression     *Expression    `json:"initialExpression,omitempty"`
	CalculatedExpression  *Expression    `json:"calculatedExpression,omitempty"`
	ItemPopulationContext *Expression    `json:"itemPopulationContext,omitempty"`
	TextRef               string         `json:"textReference,omitempty"`
	Media                 []Attachment   `json:"media,omitempty"`
}
type ResponseItem struct {
	LinkID string         `json:"linkId"`
	Text   string         `json:"text,omitempty"`
	Answer []Answer       `json:"answer,omitempty"`
	Item   []ResponseItem `json:"item,omitempty"`
}
type Answer struct {
	Value any            `json:"-"`
	Item  []ResponseItem `json:"item,omitempty"`
}
type AnswerOption struct {
	InitialSelected bool `json:"initialSelected,omitempty"`
	Value           any  `json:"-"`
}
type Coding struct {
	System  string `json:"system,omitempty"`
	Code    string `json:"code,omitempty"`
	Display string `json:"display,omitempty"`
}
type Attachment struct {
	ContentType string `json:"contentType,omitempty"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
}
type Extension struct {
	URL       string      `json:"url"`
	Value     any         `json:"-"`
	Extension []Extension `json:"extension,omitempty"`
}
type Expression struct {
	Language   string `json:"language"`
	Expression string `json:"expression,omitempty"`
	Name       string `json:"name,omitempty"`
}
type EnableWhen struct {
	Question string `json:"question"`
	Operator string `json:"operator"`
	Answer   any    `json:"-"`
}

// MarshalJSON preserves FHIR's polymorphic value[x] fields.
func (a Answer) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if a.Value != nil {
		m[valueKey(a.Value)] = a.Value
	}
	if len(a.Item) > 0 {
		m["item"] = a.Item
	}
	return json.Marshal(m)
}
func (a *Answer) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		if k == "item" {
			if err := json.Unmarshal(v, &a.Item); err != nil {
				return err
			}
		} else if strings.HasPrefix(k, "value") {
			var x any
			if err := json.Unmarshal(v, &x); err != nil {
				return err
			}
			a.Value = x
		}
	}
	return nil
}
func (a AnswerOption) MarshalJSON() ([]byte, error) {
	m := map[string]any{"initialSelected": a.InitialSelected}
	if a.Value != nil {
		m[valueKey(a.Value)] = a.Value
	}
	return json.Marshal(m)
}
func (a *AnswerOption) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		if k == "initialSelected" {
			_ = json.Unmarshal(v, &a.InitialSelected)
		} else if strings.HasPrefix(k, "value") {
			_ = json.Unmarshal(v, &a.Value)
		}
	}
	return nil
}
func (e Extension) MarshalJSON() ([]byte, error) {
	m := map[string]any{"url": e.URL}
	if e.Value != nil {
		m[valueKey(e.Value)] = e.Value
	}
	if len(e.Extension) > 0 {
		m["extension"] = e.Extension
	}
	return json.Marshal(m)
}
func (e *Extension) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		switch k {
		case "url":
			_ = json.Unmarshal(v, &e.URL)
		case "extension":
			_ = json.Unmarshal(v, &e.Extension)
		default:
			if strings.HasPrefix(k, "value") {
				_ = json.Unmarshal(v, &e.Value)
			}
		}
	}
	return nil
}
func (e EnableWhen) MarshalJSON() ([]byte, error) {
	m := map[string]any{"question": e.Question, "operator": e.Operator}
	if e.Answer != nil {
		m[valueKey(e.Answer)] = e.Answer
	}
	return json.Marshal(m)
}
func (e *EnableWhen) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		switch k {
		case "question":
			_ = json.Unmarshal(v, &e.Question)
		case "operator":
			_ = json.Unmarshal(v, &e.Operator)
		default:
			if strings.HasPrefix(k, "answer") {
				_ = json.Unmarshal(v, &e.Answer)
			}
		}
	}
	return nil
}
func valueKey(v any) string {
	switch v.(type) {
	case bool:
		return "valueBoolean"
	case float64, int, int64:
		return "valueInteger"
	case string:
		return "valueString"
	case Coding:
		return "valueCoding"
	case map[string]any:
		return "value"
	}
	return "valueString"
}

type Issue struct {
	Severity    string   `json:"severity"`
	Code        string   `json:"code"`
	Diagnostics string   `json:"diagnostics,omitempty"`
	Expression  []string `json:"expression,omitempty"`
	FieldPath   string   `json:"fieldPath,omitempty"`
}
type Outcome struct {
	ResourceType string  `json:"resourceType"`
	Issue        []Issue `json:"issue,omitempty"`
}

func (o Outcome) Error() string {
	for _, i := range o.Issue {
		if i.Severity == "error" || i.Severity == "fatal" {
			return i.Diagnostics
		}
	}
	return "sdc operation failed"
}

type Tree struct {
	Root     []Item
	ByLinkID map[string][]*Item
}

func Normalize(q Questionnaire) (Tree, error) {
	t := Tree{Root: q.Item, ByLinkID: map[string][]*Item{}}
	var walk func([]Item) error
	walk = func(items []Item) error {
		for i := range items {
			if items[i].LinkID == "" {
				return fmt.Errorf("questionnaire item has empty linkId")
			}
			t.ByLinkID[items[i].LinkID] = append(t.ByLinkID[items[i].LinkID], &items[i])
			if err := walk(items[i].Item); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(t.Root); err != nil {
		return Tree{}, err
	}
	return t, nil
}
func (t Tree) Resolve(linkID string) []*Item { return t.ByLinkID[linkID] }
func (t Tree) LinkIDs() []string {
	out := make([]string, 0, len(t.ByLinkID))
	for k := range t.ByLinkID {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type FHIRPathProvider interface {
	Evaluate(context.Context, string, any) ([]any, error)
}
type ExpressionProvider interface {
	Evaluate(context.Context, Expression, any) ([]any, error)
}
type UnsupportedProvider struct{ Language string }

func (p UnsupportedProvider) Evaluate(context.Context, Expression, any) ([]any, error) {
	return nil, fmt.Errorf("expression language %q is unavailable", p.Language)
}

type CQLProvider interface {
	EvaluateCQL(context.Context, string, any) ([]any, error)
}
type FHIRQueryProvider interface {
	ExecuteFHIRQuery(context.Context, string, any) ([]any, error)
}
type CQLExpressions struct{ Provider CQLProvider }

func (p CQLExpressions) Evaluate(ctx context.Context, e Expression, r any) ([]any, error) {
	if p.Provider == nil {
		return UnsupportedProvider{e.Language}.Evaluate(ctx, e, r)
	}
	return p.Provider.EvaluateCQL(ctx, e.Expression, r)
}

type FHIRQueryExpressions struct{ Provider FHIRQueryProvider }

func (p FHIRQueryExpressions) Evaluate(ctx context.Context, e Expression, r any) ([]any, error) {
	if p.Provider == nil {
		return UnsupportedProvider{e.Language}.Evaluate(ctx, e, r)
	}
	return p.Provider.ExecuteFHIRQuery(ctx, e.Expression, r)
}

type FHIRPathExpressions struct{ Engine fhirpath.Engine }

func (p FHIRPathExpressions) Evaluate(ctx context.Context, e Expression, r any) ([]any, error) {
	if !strings.EqualFold(e.Language, "text/fhirpath") {
		return UnsupportedProvider{e.Language}.Evaluate(ctx, e, r)
	}
	if p.Engine == nil {
		return nil, fmt.Errorf("FHIRPath engine is unavailable")
	}
	vs, err := p.Engine.Eval(ctx, e.Expression, r)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v.Raw()
	}
	return out, nil
}
