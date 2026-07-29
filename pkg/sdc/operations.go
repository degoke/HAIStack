package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
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
			if d.Type == "display" && (d.Required || d.Repeats || len(d.AnswerOption) > 0) {
				o.add("error", "invalid", "display items cannot be required, repeated, or have answer options", "Questionnaire.item["+id+"]")
			}
			if d.EnableBehavior != "" && d.EnableBehavior != "all" && d.EnableBehavior != "any" {
				o.add("error", "value", "enableBehavior must be all or any", "Questionnaire.item["+id+"]")
			}
		}
	}
	return o
}
func ValidateResponse(q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) Outcome {
	o := Outcome{ResourceType: "OperationOutcome"}
	t, err := Normalize(q)
	if err != nil {
		return Outcome{ResourceType: "OperationOutcome", Issue: []Issue{{Severity: "error", Code: "structure", Diagnostics: err.Error()}}}
	}
	if r.ResourceType != "" && r.ResourceType != "QuestionnaireResponse" {
		o.add("error", "invalid", "resourceType must be QuestionnaireResponse", "QuestionnaireResponse.resourceType")
	}
	if r.Questionnaire != "" && r.Questionnaire != Canonical(q) && r.Questionnaire != q.URL {
		o.add("error", "invariant", "response questionnaire does not match the questionnaire", "QuestionnaireResponse.questionnaire")
	}
	var walk func([]ResponseItem, string)
	walk = func(items []ResponseItem, path string) {
		for i := range items {
			p := path + "item[" + items[i].LinkID + "]"
			defs := t.Resolve(items[i].LinkID)
			if len(defs) == 0 {
				o.add("error", "not-found", "unknown linkId: "+items[i].LinkID, p)
				continue
			}
			d := defs[0]
			enabled := Enabled(*d, r)
			if d.EnableWhenExpression != nil && opts.Expressions != nil {
				vs, e := opts.Expressions.Evaluate(context.Background(), *d.EnableWhenExpression, r)
				if e != nil {
					o.add("error", "exception", e.Error(), p)
				} else {
					enabled = len(vs) > 0 && truthy(vs[0])
				}
			}
			if !enabled && len(items[i].Answer) > 0 {
				o.add("error", "invariant", "disabled item must not have an answer", p)
			}
			if d.Required && enabled && len(items[i].Answer) == 0 && !opts.AllowIncomplete {
				o.add("error", "required", "required answer is missing", p)
			}
			if !d.Repeats && len(items[i].Answer) > 1 {
				o.add("error", "max", "question does not repeat", p)
			}
			if d.ReadOnly && len(items[i].Answer) > 0 {
				o.add("error", "forbidden", "readOnly question has an answer", p)
			}
			for _, a := range items[i].Answer {
				if !answerTypeOK(d.Type, a.Value) {
					o.add("error", "type", "answer type does not match question type", p)
				}
				if opts.Terminology != nil && (d.Type == "choice" || d.Type == "open-choice") {
					if c, ok := codingFrom(a.Value); ok {
						if err := opts.Terminology.ValidateCode(context.Background(), c, d.AnswerValueSet); err != nil {
							o.add("error", "code-invalid", err.Error(), p)
						}
					}
				}
				if len(d.AnswerOption) > 0 {
					matched := false
					for _, ao := range d.AnswerOption {
						if Equal(ao.Value, a.Value) {
							matched = true
							break
						}
					}
					if !matched && d.Type != "open-choice" {
						o.add("error", "code-invalid", "answer is not one of the permitted answer options", p)
					}
				}
			}
			walk(items[i].Item, p+".")
		}
	}
	walk(r.Item, "")
	return o
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
		switch v.(type) {
		case float64, int, int64:
			return true
		}
	case "decimal":
		_, ok := v.(float64)
		return ok
	case "string", "text", "url", "date", "dateTime", "time":
		_, ok := v.(string)
		return ok
	case "choice", "open-choice":
		_, ok := codingFrom(v)
		return ok
	}
	return true
}
func codingFrom(v any) (Coding, bool) {
	switch x := v.(type) {
	case Coding:
		return x, x.Code != ""
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
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
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
		b, _ := json.Marshal(pc.InitialResponse)
		_ = json.Unmarshal(b, r)
	}
	var fill func([]Item, []ResponseItem) []ResponseItem
	fill = func(items []Item, old []ResponseItem) []ResponseItem {
		for _, it := range items {
			ri := findResponse(old, it.LinkID)
			if ri == nil {
				ri = &ResponseItem{LinkID: it.LinkID, Text: it.Text}
				ri.Answer = append(ri.Answer, it.Initial...)
				if it.InitialExpression != nil && pc.Provider != nil {
					vs, e := pc.Provider.Evaluate(ctx, *it.InitialExpression, populationRoot(pc))
					if e != nil {
						o.add("error", "exception", e.Error(), it.LinkID)
					}
					for _, v := range vs {
						ri.Answer = append(ri.Answer, Answer{Value: v})
					}
				}
				old = append(old, *ri)
			}
			if it.AnswerExpression != nil && len(ri.Answer) == 0 {
				if pc.Provider == nil {
					o.add("error", "exception", "answer expression provider is unavailable", it.LinkID)
				} else if vs, e := pc.Provider.Evaluate(ctx, *it.AnswerExpression, populationRoot(pc)); e != nil {
					o.add("error", "exception", e.Error(), it.LinkID)
				} else {
					for _, v := range vs {
						ri.Answer = append(ri.Answer, Answer{Value: v})
					}
				}
			}
			if it.ItemPopulationContext != nil && pc.Provider != nil {
				if _, e := pc.Provider.Evaluate(ctx, *it.ItemPopulationContext, populationRoot(pc)); e != nil {
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
func findResponse(items []ResponseItem, id string) *ResponseItem {
	for i := range items {
		if items[i].LinkID == id {
			return &items[i]
		}
	}
	return nil
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
	t, _ := Normalize(q)
	out := map[string][]string{}
	for id, items := range t.ByLinkID {
		for _, it := range items {
			if it.CalculatedExpression == nil {
				continue
			}
			for _, candidate := range t.LinkIDs() {
				if candidate != id && regexp.MustCompile(`\b`+regexp.QuoteMeta(candidate)+`\b`).MatchString(it.CalculatedExpression.Expression) {
					out[id] = append(out[id], candidate)
				}
			}
		}
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
	for n := range g {
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
	for n := 0; n < opts.MaxIterations; n++ {
		changed := false
		var walk func([]Item, []ResponseItem) []ResponseItem
		walk = func(items []Item, ris []ResponseItem) []ResponseItem {
			for _, it := range items {
				if it.CalculatedExpression != nil {
					vs, e := p.Evaluate(ctx, *it.CalculatedExpression, res.Response)
					if e != nil {
						res.Diagnostics = append(res.Diagnostics, Diagnostic{it.LinkID, it.CalculatedExpression.Expression, e.Error(), "error"})
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
					}
				}
				ri := findResponse(ris, it.LinkID)
				if ri != nil {
					ri.Item = walk(it.Item, ri.Item)
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
		ri := findResponse(r.Item, rule.Question)
		ok := false
		if ri != nil {
			if rule.Operator == "exists" {
				want, _ := rule.Answer.(bool)
				ok = (len(ri.Answer) > 0) == want
			}
			for _, a := range ri.Answer {
				switch rule.Operator {
				case "exists":
					want, _ := rule.Answer.(bool)
					ok = (len(ri.Answer) > 0) == want
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
	m := FormModel{Questionnaire: q, Response: r}
	var walk func([]Item)
	walk = func(items []Item) {
		for _, it := range items {
			f := FieldState{LinkID: it.LinkID, Text: it.Text, Type: it.Type, Visible: Enabled(it, r), Enabled: Enabled(it, r), Required: it.Required, ReadOnly: it.ReadOnly, Repeats: it.Repeats, Options: it.AnswerOption, Media: it.Media}
			if x := findResponse(r.Item, it.LinkID); x != nil {
				f.Answers = x.Answer
			}
			m.Fields = append(m.Fields, f)
			walk(it.Item)
		}
	}
	walk(q.Item)
	return m
}
func failed(e error) Outcome {
	return Outcome{ResourceType: "OperationOutcome", Issue: []Issue{{Severity: "error", Code: "exception", Diagnostics: e.Error()}}}
}
func (o *Outcome) add(sev, code, msg, path string) {
	o.Issue = append(o.Issue, Issue{Severity: sev, Code: code, Diagnostics: msg, Expression: []string{path}, FieldPath: path})
}
func Equal(a, b any) bool {
	return reflect.DeepEqual(a, b) || strings.EqualFold(fmt.Sprint(a), fmt.Sprint(b))
}
