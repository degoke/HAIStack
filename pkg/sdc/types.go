package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

// SDC expression extensions are FHIR extensions, not additional JSON fields
// on Questionnaire.item. These URLs are also used by Render to interpret
// common UI extensions without coupling the package to a widget framework.
const (
	SDCEnableWhenExpressionExtension  = SDCBaseURL + "sdc-questionnaire-enableWhenExpression"
	SDCInitialExpressionExtension     = SDCBaseURL + "sdc-questionnaire-initialExpression"
	SDCAnswerExpressionExtension      = SDCBaseURL + "sdc-questionnaire-answerExpression"
	SDCCalculatedExpressionExtension  = SDCBaseURL + "sdc-questionnaire-calculatedExpression"
	SDCItemPopulationContextExtension = SDCBaseURL + "sdc-questionnaire-itemPopulationContext"
	SDCItemMediaExtension             = SDCBaseURL + "sdc-questionnaire-itemMedia"
	SDCTextReferenceExtension         = SDCBaseURL + "sdc-questionnaire-textReference"
	QuestionnaireHiddenExtension      = "http://hl7.org/fhir/StructureDefinition/questionnaire-hidden"
	QuestionnaireItemControlExtension = "http://hl7.org/fhir/StructureDefinition/questionnaire-itemControl"
	QuestionnaireEntryFormatExtension = "http://hl7.org/fhir/StructureDefinition/questionnaire-entryFormat"
)

func (it Item) MarshalJSON() ([]byte, error) {
	type itemAlias Item
	encodedItem := it
	encodedItem.Initial = typedAnswers(it.Initial, it.Type)
	encodedItem.AnswerOption = typedAnswerOptions(it.AnswerOption, it.Type)
	b, err := json.Marshal(itemAlias(encodedItem))
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, err
	}

	// These are behavior projection fields. In FHIR they are represented by
	// valueExpression extensions on Questionnaire.item.
	for _, key := range []string{
		"enableWhenExpression",
		"initialExpression",
		"answerExpression",
		"calculatedExpression",
		"itemPopulationContext",
		"media",
		"textReference",
	} {
		delete(obj, key)
	}

	ext := append([]Extension(nil), it.Extension...)
	if it.EnableWhenExpression != nil {
		ext = upsertExtension(ext, Extension{URL: SDCEnableWhenExpressionExtension, Value: *it.EnableWhenExpression, valueType: "Expression"})
	}
	if it.InitialExpression != nil {
		ext = upsertExtension(ext, Extension{URL: SDCInitialExpressionExtension, Value: *it.InitialExpression, valueType: "Expression"})
	}
	if it.AnswerExpression != nil {
		ext = upsertExtension(ext, Extension{URL: SDCAnswerExpressionExtension, Value: *it.AnswerExpression, valueType: "Expression"})
	}
	if it.CalculatedExpression != nil {
		ext = upsertExtension(ext, Extension{URL: SDCCalculatedExpressionExtension, Value: *it.CalculatedExpression, valueType: "Expression"})
	}
	if it.ItemPopulationContext != nil {
		ext = upsertExtension(ext, Extension{URL: SDCItemPopulationContextExtension, Value: *it.ItemPopulationContext, valueType: "Expression"})
	}
	if len(it.Media) > 0 {
		mediaExtensions := make([]Extension, 0, len(it.Media))
		for _, media := range it.Media {
			mediaExtensions = append(mediaExtensions, Extension{URL: SDCItemMediaExtension, Value: media, valueType: "Attachment"})
		}
		ext = replaceExtensionsByURL(ext, SDCItemMediaExtension, mediaExtensions)
	}
	if it.TextRef != "" {
		ext = upsertExtension(ext, Extension{URL: SDCTextReferenceExtension, Value: it.TextRef, valueType: "String"})
	}
	if len(ext) > 0 {
		encoded, err := json.Marshal(ext)
		if err != nil {
			return nil, err
		}
		obj["extension"] = encoded
	} else {
		delete(obj, "extension")
	}
	return json.Marshal(obj)
}

func typedAnswers(answers []Answer, itemType string) []Answer {
	if len(answers) == 0 {
		return answers
	}
	out := append([]Answer(nil), answers...)
	for i := range out {
		if out[i].ValueType == "" {
			out[i].ValueType = itemValueType(itemType, out[i].Value)
		}
	}
	return out
}

func typedAnswerOptions(options []AnswerOption, itemType string) []AnswerOption {
	if len(options) == 0 {
		return options
	}
	out := append([]AnswerOption(nil), options...)
	for i := range out {
		if out[i].ValueType == "" {
			out[i].ValueType = itemValueType(itemType, out[i].Value)
		}
	}
	return out
}

func itemValueType(itemType string, value any) string {
	switch itemType {
	case "boolean":
		return "Boolean"
	case "decimal":
		return "Decimal"
	case "integer":
		return "Integer"
	case "date":
		return "Date"
	case "dateTime":
		return "DateTime"
	case "time":
		return "Time"
	case "url":
		return "Uri"
	case "quantity":
		return "Quantity"
	case "attachment":
		return "Attachment"
	case "reference":
		return "Reference"
	case "choice":
		return "Coding"
	case "open-choice":
		if _, ok := codingFrom(value); ok {
			return "Coding"
		}
		return "String"
	default:
		return "String"
	}
}

func (it *Item) UnmarshalJSON(b []byte) error {
	type itemAlias Item
	var decoded itemAlias
	if err := json.Unmarshal(b, &decoded); err != nil {
		return err
	}
	*it = Item(decoded)
	for _, ext := range it.Extension {
		switch ext.URL {
		case SDCEnableWhenExpressionExtension:
			if expression, ok := extensionExpression(ext); ok {
				it.EnableWhenExpression = &expression
			}
		case SDCInitialExpressionExtension:
			if expression, ok := extensionExpression(ext); ok {
				it.InitialExpression = &expression
			}
		case SDCAnswerExpressionExtension:
			if expression, ok := extensionExpression(ext); ok {
				it.AnswerExpression = &expression
			}
		case SDCCalculatedExpressionExtension:
			if expression, ok := extensionExpression(ext); ok {
				it.CalculatedExpression = &expression
			}
		case SDCItemPopulationContextExtension:
			if expression, ok := extensionExpression(ext); ok {
				it.ItemPopulationContext = &expression
			}
		case SDCItemMediaExtension:
			if media, ok := extensionAttachment(ext); ok {
				it.Media = append(it.Media, media)
			}
		case SDCTextReferenceExtension:
			if text, ok := ext.Value.(string); ok {
				it.TextRef = text
			}
		}
	}
	return nil
}

func extensionAttachment(ext Extension) (Attachment, bool) {
	switch x := ext.Value.(type) {
	case Attachment:
		return x, true
	case *Attachment:
		if x != nil {
			return *x, true
		}
	case map[string]any:
		b, err := json.Marshal(x)
		if err == nil {
			var attachment Attachment
			if json.Unmarshal(b, &attachment) == nil {
				return attachment, true
			}
		}
	}
	return Attachment{}, false
}

func extensionExpression(ext Extension) (Expression, bool) {
	switch x := ext.Value.(type) {
	case Expression:
		return x, true
	case *Expression:
		if x != nil {
			return *x, true
		}
	case map[string]any:
		b, err := json.Marshal(x)
		if err == nil {
			var expression Expression
			if json.Unmarshal(b, &expression) == nil {
				return expression, true
			}
		}
	}
	return Expression{}, false
}

func upsertExtension(ext []Extension, replacement Extension) []Extension {
	for i := range ext {
		if ext[i].URL == replacement.URL {
			ext[i] = replacement
			return ext
		}
	}
	return append(ext, replacement)
}

func replaceExtensionsByURL(ext []Extension, url string, replacements []Extension) []Extension {
	replacementIndex := 0
	for i := range ext {
		if ext[i].URL != url {
			continue
		}
		if replacementIndex < len(replacements) {
			ext[i] = replacements[replacementIndex]
			replacementIndex++
		}
	}
	if replacementIndex < len(replacements) {
		ext = append(ext, replacements[replacementIndex:]...)
	}
	return ext
}

type ResponseItem struct {
	LinkID string         `json:"linkId"`
	Text   string         `json:"text,omitempty"`
	Answer []Answer       `json:"answer,omitempty"`
	Item   []ResponseItem `json:"item,omitempty"`
}
type Answer struct {
	Value any `json:"-"`
	// ValueType optionally names the FHIR primitive suffix (for example Date
	// or Uri) when Go's underlying value is otherwise ambiguous.
	ValueType string         `json:"-"`
	Item      []ResponseItem `json:"item,omitempty"`
	valueType string
}
type AnswerOption struct {
	InitialSelected bool `json:"initialSelected,omitempty"`
	Value           any  `json:"-"`
	// ValueType optionally names the FHIR value[x] suffix.
	ValueType          string `json:"-"`
	valueType          string
	initialSelectedSet bool
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
	URL   string `json:"url"`
	Value any    `json:"-"`
	// ValueType optionally names the FHIR value[x] suffix for generic values.
	ValueType string      `json:"-"`
	Extension []Extension `json:"extension,omitempty"`
	valueType string
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
	// AnswerType optionally names the FHIR answer[x] suffix.
	AnswerType string `json:"-"`
	answerType string
}

// MarshalJSON preserves FHIR's polymorphic value[x] fields.
func (a Answer) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if a.Value != nil {
		m[valueKeyWithType("value", a.Value, firstNonEmpty(a.ValueType, a.valueType))] = a.Value
	}
	if len(a.Item) > 0 {
		m["item"] = a.Item
	}
	return json.Marshal(m)
}
func (a *Answer) UnmarshalJSON(b []byte) error {
	*a = Answer{}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		if k == "item" {
			if err := json.Unmarshal(v, &a.Item); err != nil {
				return err
			}
		} else if strings.HasPrefix(k, "value") && len(k) > len("value") {
			suffix := strings.TrimPrefix(k, "value")
			x, err := decodePolymorphicValue(v, suffix)
			if err != nil {
				return err
			}
			a.Value = x
			a.ValueType = suffix
			a.valueType = suffix
		}
	}
	return nil
}
func (a AnswerOption) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if a.InitialSelected || a.initialSelectedSet {
		m["initialSelected"] = true
	}
	if a.Value != nil {
		m[valueKeyWithType("value", a.Value, firstNonEmpty(a.ValueType, a.valueType))] = a.Value
	}
	return json.Marshal(m)
}
func (a *AnswerOption) UnmarshalJSON(b []byte) error {
	*a = AnswerOption{}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		if k == "initialSelected" {
			if err := json.Unmarshal(v, &a.InitialSelected); err != nil {
				return err
			}
			a.initialSelectedSet = true
		} else if strings.HasPrefix(k, "value") && len(k) > len("value") {
			suffix := strings.TrimPrefix(k, "value")
			x, err := decodePolymorphicValue(v, suffix)
			if err != nil {
				return err
			}
			a.Value = x
			a.ValueType = suffix
			a.valueType = suffix
		}
	}
	return nil
}
func (e Extension) MarshalJSON() ([]byte, error) {
	m := map[string]any{"url": e.URL}
	if e.Value != nil {
		m[valueKeyWithType("value", e.Value, firstNonEmpty(e.ValueType, e.valueType))] = e.Value
	}
	if len(e.Extension) > 0 {
		m["extension"] = e.Extension
	}
	return json.Marshal(m)
}
func (e *Extension) UnmarshalJSON(b []byte) error {
	*e = Extension{}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		switch k {
		case "url":
			if err := json.Unmarshal(v, &e.URL); err != nil {
				return err
			}
		case "extension":
			if err := json.Unmarshal(v, &e.Extension); err != nil {
				return err
			}
		default:
			if strings.HasPrefix(k, "value") && len(k) > len("value") {
				suffix := strings.TrimPrefix(k, "value")
				x, err := decodePolymorphicValue(v, suffix)
				if err != nil {
					return err
				}
				e.Value = x
				e.ValueType = suffix
				e.valueType = suffix
			}
		}
	}
	return nil
}
func (e EnableWhen) MarshalJSON() ([]byte, error) {
	m := map[string]any{"question": e.Question, "operator": e.Operator}
	if e.Answer != nil {
		m[valueKeyWithType("answer", e.Answer, firstNonEmpty(e.AnswerType, e.answerType))] = e.Answer
	}
	return json.Marshal(m)
}
func (e *EnableWhen) UnmarshalJSON(b []byte) error {
	*e = EnableWhen{}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		switch k {
		case "question":
			if err := json.Unmarshal(v, &e.Question); err != nil {
				return err
			}
		case "operator":
			if err := json.Unmarshal(v, &e.Operator); err != nil {
				return err
			}
		default:
			if strings.HasPrefix(k, "answer") && len(k) > len("answer") {
				suffix := strings.TrimPrefix(k, "answer")
				x, err := decodePolymorphicValue(v, suffix)
				if err != nil {
					return err
				}
				e.Answer = x
				e.AnswerType = suffix
				e.answerType = suffix
			}
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func valueKeyWithType(prefix string, v any, preserved string) string {
	if preserved != "" {
		return prefix + canonicalValueSuffix(preserved)
	}
	suffix := valueSuffix(v)
	return prefix + suffix
}

func canonicalValueSuffix(suffix string) string {
	switch strings.ToLower(suffix) {
	case "boolean":
		return "Boolean"
	case "integer":
		return "Integer"
	case "decimal":
		return "Decimal"
	case "string":
		return "String"
	case "date":
		return "Date"
	case "datetime":
		return "DateTime"
	case "time":
		return "Time"
	case "uri":
		return "Uri"
	case "url":
		return "Url"
	case "coding":
		return "Coding"
	case "codeableconcept":
		return "CodeableConcept"
	case "quantity":
		return "Quantity"
	case "attachment":
		return "Attachment"
	case "reference":
		return "Reference"
	case "expression":
		return "Expression"
	default:
		return suffix
	}
}

func valueSuffix(v any) string {
	switch x := v.(type) {
	case bool:
		_ = x
		return "Boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "Integer"
	case float32:
		if math.Trunc(float64(x)) == float64(x) {
			return "Integer"
		}
		return "Decimal"
	case float64:
		if math.Trunc(x) == x {
			return "Integer"
		}
		return "Decimal"
	case json.Number:
		if strings.ContainsAny(x.String(), ".eE") {
			return "Decimal"
		}
		return "Integer"
	case string:
		return "String"
	case Coding:
		return "Coding"
	case *Coding:
		return "Coding"
	case Expression:
		return "Expression"
	case *Expression:
		return "Expression"
	case Attachment:
		return "Attachment"
	case *Attachment:
		return "Attachment"
	case map[string]any:
		if _, ok := x["expression"]; ok {
			return "Expression"
		}
		if _, ok := x["coding"]; ok {
			return "CodeableConcept"
		}
		if _, ok := x["text"]; ok {
			return "CodeableConcept"
		}
		if _, ok := x["code"]; ok {
			return "Coding"
		}
		if _, ok := x["reference"]; ok {
			return "Reference"
		}
		if _, ok := x["contentType"]; ok {
			return "Attachment"
		}
		if _, ok := x["value"]; ok {
			return "Quantity"
		}
		return "String"
	}
	return "String"
}

func decodePolymorphicValue(raw json.RawMessage, suffix string) (any, error) {
	switch suffix {
	case "Coding":
		var x Coding
		if err := json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return x, nil
	case "Expression":
		var x Expression
		if err := json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return x, nil
	case "Attachment":
		var x Attachment
		if err := json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return x, nil
	default:
		var x any
		if err := json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return x, nil
	}
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
	input, err := fhirPathInput(r)
	if err != nil {
		return nil, err
	}
	vs, err := p.Engine.Eval(ctx, e.Expression, input)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = fhirPathScalar(v)
	}
	return out, nil
}

func fhirPathScalar(v fhirpath.Value) any {
	switch v.Type() {
	case "Boolean":
		if value, err := v.Bool(); err == nil {
			return value
		}
	case "String":
		if value, err := v.String(); err == nil {
			return value
		}
	case "Date", "DateTime", "Time":
		if value, err := v.String(); err == nil {
			return value
		}
	case "Integer":
		if value, err := v.Float64(); err == nil {
			return int64(value)
		}
	case "Decimal":
		if value, err := v.Float64(); err == nil {
			return value
		}
	}
	return v.Raw()
}

func fhirPathInput(input any) (any, error) {
	switch value := input.(type) {
	case QuestionnaireResponse:
		return responseExpressionEnvelope(value)
	case *QuestionnaireResponse:
		if value == nil {
			return nil, fmt.Errorf("FHIRPath evaluation input is nil")
		}
		return responseExpressionEnvelope(*value)
	case Questionnaire:
		return ProjectionEnvelope(value)
	case *Questionnaire:
		if value == nil {
			return nil, fmt.Errorf("FHIRPath evaluation input is nil")
		}
		return ProjectionEnvelope(*value)
	case map[string]any:
		resourceType, ok := value["resourceType"].(string)
		if ok && resourceType != "" {
			b, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			return types.NewJSONCodec().ParseJSON(resourceType, b)
		}
		if subject, ok := value["subject"]; ok {
			return fhirPathInput(subject)
		}
		return nil, fmt.Errorf("FHIRPath evaluation requires a ResourceEnvelope, proto, or resource-shaped map")
	default:
		return input, nil
	}
}

func responseExpressionEnvelope(r QuestionnaireResponse) (any, error) {
	env, err := ResponseProjectionEnvelope(r)
	if err != nil {
		return nil, fmt.Errorf("FHIRPath response input: %w", err)
	}
	return env, nil
}
