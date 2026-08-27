package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

type ValidationOptions struct {
	Terminology     TerminologyResolver
	AllowIncomplete bool
	Expressions     ExpressionProvider
}
type TerminologyResolver interface {
	ValidateCode(context.Context, Coding, string) error
	Display(context.Context, Coding) (string, error)
	Expand(context.Context, string) ([]Coding, error)
}
type StaticTerminology struct{ Codes map[string]map[string]string }

func (s StaticTerminology) ValidateCode(_ context.Context, c Coding, _ string) error {
	if len(s.Codes) == 0 {
		return nil
	}
	if _, ok := s.Codes[c.System][c.Code]; !ok {
		return fmt.Errorf("code %s|%s is not in the value set", c.System, c.Code)
	}
	return nil
}
func (s StaticTerminology) Display(_ context.Context, c Coding) (string, error) {
	return s.Codes[c.System][c.Code], nil
}
func (s StaticTerminology) Expand(_ context.Context, url string) ([]Coding, error) {
	return nil, fmt.Errorf("value set expansion unavailable: %s", url)
}

func ValidateQuestionnaire(q Questionnaire, opts ValidationOptions) Outcome {
	o := Outcome{ResourceType: "OperationOutcome"}
	if q.ResourceType != "" && q.ResourceType != "Questionnaire" {
		o.add("error", "invalid", "resourceType must be Questionnaire", "")
	}
	if q.URL == "" {
		o.add("error", "required", "questionnaire url is required", "Questionnaire.url")
	}
	if q.Status == "" {
		o.add("error", "required", "questionnaire status is required", "Questionnaire.status")
	} else {
		switch q.Status {
		case "draft", "active", "retired", "unknown":
		default:
			o.add("error", "value", "unsupported questionnaire status: "+q.Status, "Questionnaire.status")
		}
	}
	t, err := Normalize(q)
	if err != nil {
		o.add("error", "structure", err.Error(), "Questionnaire.item")
		return o
	}
	for _, id := range t.LinkIDs() {
		if len(t.Resolve(id)) > 1 {
			o.add("error", "duplicate", "duplicate linkId: "+id, "Questionnaire.item")
		}
	}
	validTypes := map[string]bool{"group": true, "display": true, "boolean": true, "decimal": true, "integer": true, "date": true, "dateTime": true, "time": true, "string": true, "text": true, "url": true, "choice": true, "open-choice": true, "quantity": true, "attachment": true, "reference": true}
	for _, id := range t.LinkIDs() {
		for _, d := range t.Resolve(id) {
			if !validTypes[d.Type] {
				o.add("error", "value", "unsupported questionnaire item type: "+d.Type, "Questionnaire.item["+id+"]")
			}
			if d.Type == "display" && (d.Required || d.Repeats || len(d.AnswerOption) > 0 || len(d.Initial) > 0) {
				o.add("error", "invalid", "display items cannot be required, repeated, or have answer options", "Questionnaire.item["+id+"]")
			}
			if d.Type == "group" && (len(d.AnswerOption) > 0 || len(d.Initial) > 0) {
				o.add("error", "invalid", "group items cannot have answers or initial values", "Questionnaire.item["+id+"]")
			}
			if d.EnableBehavior != "" && d.EnableBehavior != "all" && d.EnableBehavior != "any" {
				o.add("error", "value", "enableBehavior must be all or any", "Questionnaire.item["+id+"]")
			}
			if len(d.EnableWhen) > 1 && d.EnableBehavior == "" {
				o.add("error", "required", "enableBehavior is required when multiple enableWhen rules are present", "Questionnaire.item["+id+"]")
			}
			if len(d.EnableWhen) == 0 && d.EnableBehavior != "" {
				o.add("error", "invalid", "enableBehavior requires enableWhen", "Questionnaire.item["+id+"]")
			}
			if d.EnableWhenExpression != nil && len(d.EnableWhen) > 0 {
				o.add("error", "invariant", "enableWhen and enableWhenExpression are mutually exclusive", "Questionnaire.item["+id+"]")
			}
			if d.InitialExpression != nil && len(d.Initial) > 0 {
				o.add("error", "invariant", "initial and initialExpression are mutually exclusive", "Questionnaire.item["+id+"]")
			}
			if d.AnswerExpression != nil && (len(d.AnswerOption) > 0 || d.AnswerValueSet != "") {
				o.add("error", "invariant", "answerExpression cannot be combined with answer options or answerValueSet", "Questionnaire.item["+id+"]")
			}
			for _, expression := range []*Expression{d.EnableWhenExpression, d.AnswerExpression, d.InitialExpression, d.CalculatedExpression, d.ItemPopulationContext} {
				if expression != nil && (expression.Language == "" || expression.Expression == "") {
					o.add("error", "required", "expression language and expression are required", "Questionnaire.item["+id+"]")
				}
			}
			for _, rule := range d.EnableWhen {
				path := "Questionnaire.item[" + id + "].enableWhen"
				var target *Item
				if rule.Question == "" {
					o.add("error", "required", "enableWhen question is required", path)
				} else if rule.Question == id {
					o.add("error", "invariant", "enableWhen cannot reference itself", path)
				} else if len(t.Resolve(rule.Question)) == 0 {
					o.add("error", "not-found", "enableWhen question not found: "+rule.Question, path)
				} else {
					target = t.Resolve(rule.Question)[0]
				}
				switch rule.Operator {
				case "exists":
					if _, ok := rule.Answer.(bool); !ok {
						o.add("error", "type", "enableWhen exists answer must be boolean", path)
					}
				case "=", "!=", ">", "<", ">=", "<=":
					if rule.Answer == nil {
						o.add("error", "required", "enableWhen answer is required", path)
					}
				default:
					o.add("error", "value", "unsupported enableWhen operator: "+rule.Operator, path)
				}
				if target != nil && rule.Operator != "exists" && rule.Answer != nil && !answerTypeOK(target.Type, rule.Answer) {
					o.add("error", "type", "enableWhen answer does not match referenced question type", path)
				}
			}
			for _, a := range append(append([]Answer(nil), d.Initial...), answerOptionsAsAnswers(d.AnswerOption)...) {
				if !answerTypeOK(d.Type, a.Value) {
					o.add("error", "type", "initial or option value does not match question type", "Questionnaire.item["+id+"]")
				}
			}
			for _, ext := range d.Extension {
				if ext.URL == "" {
					o.add("error", "required", "questionnaire extension url is required", "Questionnaire.item["+id+"]")
				}
				if ext.Value == nil && len(ext.Extension) == 0 {
					o.add("error", "structure", "questionnaire extension must have a value or nested extensions", "Questionnaire.item["+id+"]")
				}
				if ext.Value != nil && len(ext.Extension) > 0 {
					o.add("error", "structure", "questionnaire extension cannot have both a value and nested extensions", "Questionnaire.item["+id+"]")
				}
			}
		}
	}
	validateExtensions := func(extensions []Extension, path string) {
		for _, ext := range extensions {
			if ext.URL == "" {
				o.add("error", "required", "questionnaire extension url is required", path)
			}
			if ext.Value == nil && len(ext.Extension) == 0 {
				o.add("error", "structure", "questionnaire extension must have a value or nested extensions", path)
			}
			if ext.Value != nil && len(ext.Extension) > 0 {
				o.add("error", "structure", "questionnaire extension cannot have both a value and nested extensions", path)
			}
		}
	}
	validateExtensions(q.Extension, "Questionnaire.extension")
	return o
}
func ValidateResponse(q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) Outcome {
	o := Outcome{ResourceType: "OperationOutcome"}
	t, err := Normalize(q)
	if err != nil {
		return Outcome{ResourceType: "OperationOutcome", Issue: []Issue{{Severity: "error", Code: "structure", Diagnostics: err.Error()}}}
	}
	for _, linkID := range t.LinkIDs() {
		if len(t.Resolve(linkID)) > 1 {
			o.add("error", "duplicate", "duplicate linkId: "+linkID, "Questionnaire.item")
		}
	}
	if r.ResourceType != "" && r.ResourceType != "QuestionnaireResponse" {
		o.add("error", "invalid", "resourceType must be QuestionnaireResponse", "QuestionnaireResponse.resourceType")
	}
	if r.Status == "" {
		o.add("error", "required", "questionnaire response status is required", "QuestionnaireResponse.status")
	} else {
		switch r.Status {
		case "in-progress", "completed", "amended", "entered-in-error", "stopped":
		default:
			o.add("error", "value", "unsupported questionnaire response status: "+r.Status, "QuestionnaireResponse.status")
		}
	}
	if r.Questionnaire != "" && r.Questionnaire != Canonical(q) && r.Questionnaire != q.URL {
		o.add("error", "invariant", "response questionnaire does not match the questionnaire", "QuestionnaireResponse.questionnaire")
	}
	var validateLevel func([]Item, []ResponseItem, string)
	validateLevel = func(questionItems []Item, responseItems []ResponseItem, path string) {
		expected := map[string]struct{}{}
		for _, item := range questionItems {
			expected[item.LinkID] = struct{}{}
		}
		for i := range responseItems {
			responseItem := &responseItems[i]
			p := path + "item[" + responseItem.LinkID + "]"
			defs := t.Resolve(responseItem.LinkID)
			if len(defs) == 0 {
				o.add("error", "not-found", "unknown linkId: "+responseItem.LinkID, p)
				continue
			}
			d := defs[0]
			if _, ok := expected[responseItem.LinkID]; !ok {
				o.add("error", "structure", "response item is not allowed at this level", p)
			}
			validateResponseItem(&o, d, responseItem, r, opts, p)
			validateLevel(d.Item, responseItem.Item, p+".")
			for ai, answer := range responseItem.Answer {
				validateLevel(d.Item, answer.Item, fmt.Sprintf("%s.answer[%d].", p, ai))
			}
		}
		for _, item := range questionItems {
			matches := directResponses(responseItems, item.LinkID)
			if len(matches) == 0 {
				enabled, expressionErr := enabledForValidationOutcome(item, r, opts)
				if expressionErr != nil {
					o.add("error", "exception", expressionErr.Error(), path+"item["+item.LinkID+"]")
				}
				if item.Required && enabled && !opts.AllowIncomplete {
					o.add("error", "required", "required answer is missing", path+"item["+item.LinkID+"]")
				}
			} else if !item.Repeats && len(matches) > 1 {
				o.add("error", "max", "question item does not repeat", path+"item["+item.LinkID+"]")
			}
		}
	}
	validateLevel(q.Item, r.Item, "")
	return o
}

func validateResponseItem(o *Outcome, d *Item, responseItem *ResponseItem, r QuestionnaireResponse, opts ValidationOptions, path string) {
	enabled, expressionErr := enabledForValidationOutcome(*d, r, opts)
	if expressionErr != nil {
		o.add("error", "exception", expressionErr.Error(), path)
	}
	if !enabled && (hasPresentAnswers(responseItem.Answer) || len(responseItem.Item) > 0) {
		o.add("error", "invariant", fmt.Sprintf("disabled item %q must not contain answers or child items (enableWhen: %s)", d.LinkID, enableWhenSummary(*d)), path)
	}
	if d.Required && enabled && !hasPresentAnswers(responseItem.Answer) && !opts.AllowIncomplete && d.Type != "group" {
		o.add("error", "required", "required answer is missing", path)
	}
	if !d.Repeats && countPresentAnswers(responseItem.Answer) > 1 {
		o.add("error", "max", "question does not repeat", path)
	}
	if d.ReadOnly && hasPresentAnswers(responseItem.Answer) {
		o.add("error", "forbidden", "readOnly question has an answer", path)
	}
	for _, answer := range responseItem.Answer {
		if answerValueAbsent(answer.Value) {
			continue
		}
		if !answerTypeOK(d.Type, answer.Value) {
			o.add("error", "type", "answer type does not match question type", path)
		}
		if opts.Terminology != nil && (d.Type == "choice" || d.Type == "open-choice") {
			if c, ok := codingFrom(answer.Value); ok {
				if err := opts.Terminology.ValidateCode(context.Background(), c, d.AnswerValueSet); err != nil {
					o.add("error", "code-invalid", err.Error(), path)
				}
			}
		}
		if len(d.AnswerOption) > 0 {
			matched := false
			for _, option := range d.AnswerOption {
				if Equal(option.Value, answer.Value) {
					matched = true
					break
				}
			}
			if !matched && d.Type != "open-choice" {
				o.add("error", "code-invalid", "answer is not one of the permitted answer options", path)
			}
		}
	}
}

func enableWhenSummary(item Item) string {
	if len(item.EnableWhen) == 0 {
		if item.EnableWhenExpression != nil {
			return "expression"
		}
		return "none"
	}
	parts := make([]string, 0, len(item.EnableWhen))
	for _, rule := range item.EnableWhen {
		parts = append(parts, rule.Question+" "+rule.Operator)
	}
	return strings.Join(parts, ", ")
}

func enabledForValidation(item Item, r QuestionnaireResponse, opts ValidationOptions) bool {
	enabled, _ := enabledForValidationOutcome(item, r, opts)
	return enabled
}

func enabledForValidationOutcome(item Item, r QuestionnaireResponse, opts ValidationOptions) (bool, error) {
	enabled := Enabled(item, r)
	if item.EnableWhenExpression != nil {
		if opts.Expressions == nil {
			return false, fmt.Errorf("enableWhen expression provider is unavailable")
		}
		values, err := opts.Expressions.Evaluate(context.Background(), *item.EnableWhenExpression, r)
		if err != nil {
			return false, err
		}
		enabled = len(values) > 0 && truthy(values[0])
	}
	return enabled, nil
}

func answerOptionsAsAnswers(options []AnswerOption) []Answer {
	answers := make([]Answer, 0, len(options))
	for _, option := range options {
		answers = append(answers, Answer{Value: option.Value})
	}
	return answers
}

func directResponses(items []ResponseItem, linkID string) []*ResponseItem {
	var matches []*ResponseItem
	for i := range items {
		if items[i].LinkID == linkID {
			matches = append(matches, &items[i])
		}
	}
	return matches
}
func answerTypeOK(typ string, v any) bool {
	if v == nil {
		return false
	}
	switch typ {
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		n, ok := number(v)
		return ok && math.Trunc(n) == n && n >= math.MinInt32 && n <= math.MaxInt32
	case "decimal":
		_, ok := number(v)
		return ok
	case "string", "text", "url", "date", "dateTime", "time":
		_, ok := v.(string)
		return ok
	case "choice", "open-choice":
		if _, ok := codingFrom(v); ok {
			return true
		}
		return typ == "open-choice" && reflect.TypeOf(v).Kind() == reflect.String
	case "quantity":
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if value, exists := m["value"]; exists {
			_, ok = number(value)
			return ok
		}
		return false
	case "attachment", "reference":
		if _, ok := v.(map[string]any); ok {
			return true
		}
		if typ == "attachment" {
			switch v.(type) {
			case Attachment, *Attachment:
				return true
			}
		}
		return false
	case "group", "display":
		return false
	}
	return false
}
func codingFrom(v any) (Coding, bool) {
	switch x := v.(type) {
	case Coding:
		return x, x.Code != ""
	case *Coding:
		if x != nil {
			return *x, x.Code != ""
		}
	case map[string]any:
		c := Coding{}
		if s, ok := x["system"].(string); ok {
			c.System = s
		}
		if s, ok := x["code"].(string); ok {
			c.Code = s
		}
		if s, ok := x["display"].(string); ok {
			c.Display = s
		}
		return c, c.Code != ""
	}
	return Coding{}, false
}
func truthy(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return v != nil
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case float32:
		f := float64(n)
		return f, !math.IsNaN(f) && !math.IsInf(f, 0)
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

type PopulationContext struct {
	Subject            any
	LaunchContext      map[string]any
	InitialResponse    *QuestionnaireResponse
	Provider           ExpressionProvider
	PopulationProvider PopulationProvider
}
type PopulationProvider interface {
	Populate(context.Context, Questionnaire, PopulationContext) (*QuestionnaireResponse, error)
}

func Populate(ctx context.Context, q Questionnaire, pc PopulationContext) (*QuestionnaireResponse, Outcome) {
	o := Outcome{ResourceType: "OperationOutcome"}
	if pc.PopulationProvider != nil {
		r, e := pc.PopulationProvider.Populate(ctx, q, pc)
		if e != nil {
			return nil, failed(e)
		}
		return r, o
	}
	r := &QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Questionnaire: Canonical(q), Status: "in-progress"}
	if pc.InitialResponse != nil {
		r.ID = pc.InitialResponse.ID
		r.Subject = pc.InitialResponse.Subject
		r.Authored = pc.InitialResponse.Authored
		r.Item = append([]ResponseItem(nil), pc.InitialResponse.Item...)
	}
	var fill func([]Item, []ResponseItem) []ResponseItem
	fill = func(items []Item, old []ResponseItem) []ResponseItem {
		for _, it := range items {
			ri := findResponse(old, it.LinkID)
			if ri == nil {
				old = append(old, ResponseItem{LinkID: it.LinkID, Text: it.Text})
				ri = &old[len(old)-1]
			}
			if len(ri.Answer) == 0 {
				ri.Answer = append(ri.Answer, it.Initial...)
				if it.InitialExpression != nil {
					if pc.Provider == nil {
						o.add("error", "exception", "initial expression provider is unavailable", it.LinkID)
					} else {
						vs, e := pc.Provider.Evaluate(ctx, *it.InitialExpression, populationExpressionInput(pc))
						if e != nil {
							o.add("error", "exception", e.Error(), it.LinkID)
						}
						for _, v := range vs {
							ri.Answer = append(ri.Answer, Answer{Value: v})
						}
					}
				}
			}
			if it.AnswerExpression != nil && len(ri.Answer) == 0 {
				if pc.Provider == nil {
					o.add("error", "exception", "answer expression provider is unavailable", it.LinkID)
				} else if vs, e := pc.Provider.Evaluate(ctx, *it.AnswerExpression, populationExpressionInput(pc)); e != nil {
					o.add("error", "exception", e.Error(), it.LinkID)
				} else {
					for _, v := range vs {
						ri.Answer = append(ri.Answer, Answer{Value: v})
					}
				}
			}
			if it.ItemPopulationContext != nil {
				if pc.Provider == nil {
					o.add("error", "exception", "item population context provider is unavailable", it.LinkID)
				} else if _, e := pc.Provider.Evaluate(ctx, *it.ItemPopulationContext, populationExpressionInput(pc)); e != nil {
					o.add("error", "exception", e.Error(), it.LinkID)
				}
			}
			ri.Item = fill(it.Item, ri.Item)
		}
		return old
	}
	r.Item = fill(q.Item, r.Item)
	return r, o
}
func populationRoot(pc PopulationContext) any {
	return map[string]any{"subject": pc.Subject, "launchContext": pc.LaunchContext}
}

func populationExpressionInput(pc PopulationContext) any {
	// FHIRPath evaluates a FHIR resource root. The generic map is retained for
	// CQL/FHIR Query adapters, while the built-in FHIRPath adapter evaluates the
	// supplied subject resource directly.
	if pc.Subject != nil {
		return pc.Subject
	}
	return populationRoot(pc)
}
func findResponse(items []ResponseItem, id string) *ResponseItem {
	for i := range items {
		if items[i].LinkID == id {
			return &items[i]
		}
	}
	return nil
}

func findResponsesDeep(items []ResponseItem, id string) []*ResponseItem {
	var out []*ResponseItem
	for i := range items {
		if items[i].LinkID == id {
			out = append(out, &items[i])
		}
		out = append(out, findResponsesDeep(items[i].Item, id)...)
		for _, answer := range items[i].Answer {
			out = append(out, findResponsesDeep(answer.Item, id)...)
		}
	}
	return out
}

func findResponseDeep(items []ResponseItem, id string) *ResponseItem {
	responses := findResponsesDeep(items, id)
	if len(responses) == 0 {
		return nil
	}
	return responses[0]
}

type CalculatedOptions struct{ MaxIterations int }
type Diagnostic struct {
	LinkID     string
	Expression string
	Message    string
	Severity   string
}
type CalculationResult struct {
	Response    QuestionnaireResponse
	Diagnostics []Diagnostic
	Converged   bool
}

// ExpressionDependencies returns the questionnaire linkIds referenced by
// calculated expressions. It is intentionally conservative: unknown tokens
// are ignored, while known linkIds are edges in the dependency graph.
func ExpressionDependencies(q Questionnaire) map[string][]string {
	t, err := Normalize(q)
	out := map[string][]string{}
	if err != nil {
		return out
	}
	linkIDs := t.LinkIDs()
	patterns := make(map[string]*regexp.Regexp, len(linkIDs))
	for _, candidate := range linkIDs {
		// This remains a conservative lexical heuristic; dynamic linkIds and
		// variables cannot be recovered from expression text alone.
		patterns[candidate] = regexp.MustCompile(`\b` + regexp.QuoteMeta(candidate) + `\b`)
	}
	for id, items := range t.ByLinkID {
		for _, it := range items {
			if it.CalculatedExpression == nil {
				continue
			}
			for _, candidate := range linkIDs {
				if candidate != id && patterns[candidate].MatchString(it.CalculatedExpression.Expression) {
					out[id] = append(out[id], candidate)
				}
			}
		}
	}
	for id := range out {
		sort.Strings(out[id])
	}
	return out
}
func DependencyCycles(q Questionnaire) [][]string {
	g := ExpressionDependencies(q)
	state := map[string]int{}
	stack := []string{}
	var cycles [][]string
	var visit func(string)
	visit = func(n string) {
		if state[n] == 1 {
			for i := range stack {
				if stack[i] == n {
					cycles = append(cycles, append([]string(nil), stack[i:]...))
					break
				}
			}
			return
		}
		if state[n] == 2 {
			return
		}
		state[n] = 1
		stack = append(stack, n)
		for _, d := range g[n] {
			visit(d)
		}
		stack = stack[:len(stack)-1]
		state[n] = 2
	}
	nodes := make([]string, 0, len(g))
	for n := range g {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		visit(n)
	}
	return cycles
}

func EvaluateCalculated(ctx context.Context, q Questionnaire, r QuestionnaireResponse, p ExpressionProvider, opts CalculatedOptions) CalculationResult {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 8
	}
	res := CalculationResult{Response: r}
	if cycles := DependencyCycles(q); len(cycles) > 0 {
		for _, cycle := range cycles {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{LinkID: strings.Join(cycle, " -> "), Message: "calculated expression dependency cycle", Severity: "error"})
		}
		return res
	}
	if p == nil {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{Message: "expression provider is unavailable", Severity: "error"})
		return res
	}
	diagnosticSeen := map[string]struct{}{}
	for n := 0; n < opts.MaxIterations; n++ {
		changed := false
		var walk func([]Item, []ResponseItem) []ResponseItem
		walk = func(items []Item, ris []ResponseItem) []ResponseItem {
			for _, it := range items {
				if it.CalculatedExpression != nil {
					vs, e := p.Evaluate(ctx, *it.CalculatedExpression, res.Response)
					if e != nil {
						key := it.LinkID + "\x00" + it.CalculatedExpression.Expression + "\x00" + e.Error()
						if _, seen := diagnosticSeen[key]; !seen {
							diagnosticSeen[key] = struct{}{}
							res.Diagnostics = append(res.Diagnostics, Diagnostic{it.LinkID, it.CalculatedExpression.Expression, e.Error(), "error"})
						}
					} else if len(vs) > 0 {
						ri := findResponse(ris, it.LinkID)
						if ri == nil {
							ris = append(ris, ResponseItem{LinkID: it.LinkID})
							ri = findResponse(ris, it.LinkID)
						}
						a := make([]Answer, 0, len(vs))
						for _, v := range vs {
							a = append(a, Answer{Value: v})
						}
						if !reflect.DeepEqual(ri.Answer, a) {
							ri.Answer = a
							changed = true
						}
					} else if ri := findResponse(ris, it.LinkID); ri != nil && len(ri.Answer) > 0 {
						ri.Answer = nil
						changed = true
					}
				}
				ri := findResponse(ris, it.LinkID)
				if ri != nil {
					ri.Item = walk(it.Item, ri.Item)
				} else if len(it.Item) > 0 {
					children := walk(it.Item, nil)
					if len(children) > 0 {
						ris = append(ris, ResponseItem{LinkID: it.LinkID, Item: children})
					}
				}
			}
			return ris
		}
		res.Response.Item = walk(q.Item, res.Response.Item)
		if !changed {
			res.Converged = true
			return res
		}
	}
	res.Diagnostics = append(res.Diagnostics, Diagnostic{Message: "calculated expressions did not converge", Severity: "error"})
	return res
}

type FieldState struct {
	LinkID         string
	Text           string
	Type           string
	Visible        bool
	Enabled        bool
	Required       bool
	ReadOnly       bool
	Repeats        bool
	Answers        []Answer
	Options        []AnswerOption
	Issues         []Issue
	Media          []Attachment
	ItemControl    string
	EntryFormat    string
	NavigationHint string
}
type FormModel struct {
	Questionnaire Questionnaire
	Response      QuestionnaireResponse
	Fields        []FieldState
	Issues        []Issue
}

func Enabled(item Item, r QuestionnaireResponse) bool {
	if len(item.EnableWhen) == 0 {
		return true
	}
	matched := 0
	for _, rule := range item.EnableWhen {
		ri := findResponseDeep(r.Item, rule.Question)
		ok := false
		if rule.Operator == "exists" {
			want, _ := rule.Answer.(bool)
			ok = (ri != nil && hasPresentAnswers(ri.Answer)) == want
		} else if ri != nil {
			for _, a := range ri.Answer {
				switch rule.Operator {
				case "=":
					ok = Equal(a.Value, rule.Answer)
				case "!=":
					ok = !Equal(a.Value, rule.Answer)
				case ">", "<", ">=", "<=":
					av, aok := number(a.Value)
					bv, bok := number(rule.Answer)
					if aok && bok {
						switch rule.Operator {
						case ">":
							ok = av > bv
						case "<":
							ok = av < bv
						case ">=":
							ok = av >= bv
						case "<=":
							ok = av <= bv
						}
					}
				}
				if ok {
					break
				}
			}
		}
		if ok {
			matched++
		}
	}
	if strings.EqualFold(item.EnableBehavior, "any") {
		return matched > 0
	}
	return matched == len(item.EnableWhen)
}

func Render(q Questionnaire, r QuestionnaireResponse) FormModel {
	return RenderWithOptions(q, r, ValidationOptions{})
}

// RenderWithOptions evaluates expression-based enablement and attaches
// response-validation issues to the corresponding fields.
func RenderWithOptions(q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) FormModel {
	m := FormModel{Questionnaire: q, Response: r}
	validation := ValidateResponse(q, r, opts)
	var walk func([]Item)
	walk = func(items []Item) {
		for _, it := range items {
			enabled := enabledForValidation(it, r, opts)
			visible := enabled && !extensionBool(it.Extension, QuestionnaireHiddenExtension)
			f := FieldState{
				LinkID:      it.LinkID,
				Text:        it.Text,
				Type:        it.Type,
				Visible:     visible,
				Enabled:     enabled,
				Required:    it.Required,
				ReadOnly:    it.ReadOnly,
				Repeats:     it.Repeats,
				Options:     it.AnswerOption,
				Media:       it.Media,
				ItemControl: extensionString(it.Extension, QuestionnaireItemControlExtension),
				EntryFormat: extensionString(it.Extension, QuestionnaireEntryFormatExtension),
			}
			for _, x := range findResponsesDeep(r.Item, it.LinkID) {
				f.Answers = append(f.Answers, x.Answer...)
			}
			for _, issue := range validation.Issue {
				if strings.Contains(issue.FieldPath, "item["+it.LinkID+"]") {
					f.Issues = append(f.Issues, issue)
				}
			}
			m.Fields = append(m.Fields, f)
			walk(it.Item)
		}
	}
	walk(q.Item)
	m.Issues = validation.Issue
	return m
}

func extensionBool(extensions []Extension, url string) bool {
	for _, ext := range extensions {
		if ext.URL != url {
			continue
		}
		if value, ok := ext.Value.(bool); ok {
			return value
		}
	}
	return false
}

func extensionString(extensions []Extension, url string) string {
	for _, ext := range extensions {
		if ext.URL != url {
			continue
		}
		if value, ok := ext.Value.(string); ok {
			return value
		}
		if value, ok := ext.Value.(map[string]any); ok {
			if text, ok := value["text"].(string); ok {
				return text
			}
			if coding, ok := value["coding"].([]any); ok && len(coding) > 0 {
				if first, ok := coding[0].(map[string]any); ok {
					if code, ok := first["code"].(string); ok {
						return code
					}
				}
			}
		}
	}
	return ""
}
func failed(e error) Outcome {
	return Outcome{ResourceType: "OperationOutcome", Issue: []Issue{{Severity: "error", Code: "exception", Diagnostics: e.Error()}}}
}

// answerValueAbsent reports whether an answer value is treated as absent for
// validation. Blank primitive strings and codings without a code are absent.
func answerValueAbsent(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	}
	if c, ok := codingFrom(v); ok {
		return strings.TrimSpace(c.Code) == ""
	}
	return false
}

func hasPresentAnswers(answers []Answer) bool {
	for _, answer := range answers {
		if !answerValueAbsent(answer.Value) {
			return true
		}
	}
	return false
}

func countPresentAnswers(answers []Answer) int {
	count := 0
	for _, answer := range answers {
		if !answerValueAbsent(answer.Value) {
			count++
		}
	}
	return count
}
func (o *Outcome) add(sev, code, msg, path string) {
	o.Issue = append(o.Issue, Issue{Severity: sev, Code: code, Diagnostics: msg, Expression: []string{path}, FieldPath: path})
}
func Equal(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if av, aok := number(a); aok {
		if bv, bok := number(b); bok {
			return av == bv
		}
	}
	if ac, aok := codingFrom(a); aok {
		if bc, bok := codingFrom(b); bok {
			return ac.System == bc.System && ac.Code == bc.Code && ac.Display == bc.Display
		}
	}
	return false
}
