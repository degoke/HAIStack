package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type QuestionnaireResolver interface {
	Resolve(context.Context, string) (Questionnaire, error)
}
type Assembler struct{ Resolver QuestionnaireResolver }

// AssembleResource performs modular assembly for a canonical Questionnaire
// envelope using this assembler's resolver.
func (a Assembler) AssembleResource(ctx context.Context, env *types.ResourceEnvelope) (*types.ResourceEnvelope, Outcome) {
	return AssembleQuestionnaireResource(ctx, env, a.Resolver)
}

func (a Assembler) Assemble(ctx context.Context, q Questionnaire) (Questionnaire, Outcome) {
	o := ValidateQuestionnaire(q, ValidationOptions{})
	if len(o.Issue) > 0 {
		return q, o
	}
	out := q
	resolving := map[string]bool{}
	var expand func([]Item) []Item
	expand = func(items []Item) []Item {
		var r []Item
		for _, it := range items {
			if strings.TrimSpace(it.Definition) != "" {
				if a.Resolver == nil {
					o.add("error", "exception", "questionnaire resolver is unavailable", it.LinkID)
					r = append(r, it)
					continue
				}
				if resolving[it.Definition] {
					o.add("error", "invariant", "cyclic questionnaire definition: "+it.Definition, it.LinkID)
					r = append(r, it)
					continue
				}
				resolving[it.Definition] = true
				ref, e := a.Resolver.Resolve(ctx, it.Definition)
				delete(resolving, it.Definition)
				if e != nil {
					o.add("error", "not-found", e.Error(), it.LinkID)
					r = append(r, it)
					continue
				}
				r = append(r, expand(ref.Item)...)
				continue
			}
			it.Item = expand(it.Item)
			r = append(r, it)
		}
		return r
	}
	out.Item = expand(out.Item)
	if _, e := Normalize(out); e != nil {
		o.add("error", "duplicate", e.Error(), "Questionnaire.item")
	}
	if len(o.Issue) == 0 {
		if final := ValidateQuestionnaire(out, ValidationOptions{}); len(final.Issue) > 0 {
			o.Issue = append(o.Issue, final.Issue...)
		}
	}
	return out, o
}

type ExtractionDiagnostic struct {
	Severity string
	Message  string
	LinkID   string
}
type ExtractionResult struct {
	// Bundle is the canonical FHIR transaction Bundle envelope.
	Bundle      *types.ResourceEnvelope
	Diagnostics []ExtractionDiagnostic
}
type Extractor interface {
	Extract(context.Context, Questionnaire, QuestionnaireResponse) (ExtractionResult, error)
}
type DefinitionMap struct {
	LinkID       string
	ResourceType string
	Path         string
	ValuePath    string
	Identity     string
}
type DefinitionExtractor struct {
	Mappings []DefinitionMap
	Existing func(context.Context, string, string) (bool, error)
}

func (e DefinitionExtractor) Extract(ctx context.Context, q Questionnaire, r QuestionnaireResponse) (ExtractionResult, error) {
	var entries []map[string]any
	_ = q
	mappings := append([]DefinitionMap(nil), e.Mappings...)
	sort.SliceStable(mappings, func(i, j int) bool {
		a, b := mappings[i], mappings[j]
		if a.LinkID != b.LinkID {
			return a.LinkID < b.LinkID
		}
		if a.ResourceType != b.ResourceType {
			return a.ResourceType < b.ResourceType
		}
		return a.Path < b.Path
	})
	linkMappingCount := map[string]int{}
	for _, m := range mappings {
		linkMappingCount[m.LinkID]++
	}
	linkMappingOrdinal := map[string]int{}
	seenFullURLs := map[string]int{}
	for _, m := range mappings {
		if m.ResourceType == "" {
			return ExtractionResult{}, fmt.Errorf("definition mapping %q has no resource type", m.LinkID)
		}
		responses := findResponsesDeep(r.Item, m.LinkID)
		if len(responses) == 0 {
			continue
		}
		linkMappingOrdinal[m.LinkID]++
		mappingOrdinal := linkMappingOrdinal[m.LinkID]
		for responseOrdinal, ri := range responses {
			for n, answer := range ri.Answer {
				answerValue := answer.Value
				if m.ValuePath != "" {
					var ok bool
					answerValue, ok = valueAtPath(answer.Value, m.ValuePath)
					if !ok {
						return ExtractionResult{}, fmt.Errorf("value path %q not found for mapping %q", m.ValuePath, m.LinkID)
					}
				}
				res := map[string]any{"resourceType": m.ResourceType}
				if m.Identity != "" {
					identity := strings.TrimPrefix(m.Identity, m.ResourceType+"/")
					if identity != "" {
						res["id"] = identity
					}
				}
				if m.Path != "" {
					setPath(res, m.Path, answerValue)
				}
				b, _ := json.Marshal(res)
				id, _ := res["id"].(string)
				method, url := "POST", m.ResourceType
				if id != "" {
					url += "/" + id
				}
				if e.Existing != nil && id != "" {
					ok, err := e.Existing(ctx, m.ResourceType, id)
					if err != nil {
						return ExtractionResult{}, err
					}
					if ok {
						method = "PUT"
					}
				}
				full := "urn:uuid:" + m.LinkID
				if linkMappingCount[m.LinkID] > 1 {
					full = fmt.Sprintf("%s-m%d", full, mappingOrdinal)
				}
				if len(responses) > 1 {
					full = fmt.Sprintf("%s-r%d", full, responseOrdinal)
				}
				if n > 0 {
					full = fmt.Sprintf("%s-%d", full, n)
				}
				if seenFullURLs[full] > 0 {
					seenFullURLs[full]++
					full = fmt.Sprintf("%s-%d", full, seenFullURLs[full])
				}
				seenFullURLs[full] = 1
				entries = append(entries, map[string]any{"fullUrl": full, "resource": json.RawMessage(b), "request": map[string]any{"method": method, "url": url}})
			}
		}
	}
	env, err := transactionEnvelope(entries)
	if err != nil {
		return ExtractionResult{}, err
	}
	return ExtractionResult{Bundle: env}, nil
}

func valueAtPath(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var current any
	if err := json.Unmarshal(b, &current); err != nil {
		return nil, false
	}
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = v
			return
		}
		n, ok := cur[p].(map[string]any)
		if !ok {
			n = map[string]any{}
			cur[p] = n
		}
		cur = n
	}
}

type TemplateExtractor struct {
	Template func(context.Context, QuestionnaireResponse) ([]json.RawMessage, error)
}

func (t TemplateExtractor) Extract(ctx context.Context, _ Questionnaire, r QuestionnaireResponse) (ExtractionResult, error) {
	if t.Template == nil {
		return ExtractionResult{}, fmt.Errorf("template extractor is unavailable")
	}
	rs, e := t.Template(ctx, r)
	if e != nil {
		return ExtractionResult{}, e
	}
	var entries []map[string]any
	for _, raw := range rs {
		var h struct {
			ResourceType string `json:"resourceType"`
			ID           string `json:"id,omitempty"`
		}
		if json.Unmarshal(raw, &h) != nil || h.ResourceType == "" {
			return ExtractionResult{}, fmt.Errorf("template produced invalid FHIR resource")
		}
		method, url := "POST", h.ResourceType
		if h.ID != "" {
			method, url = "PUT", h.ResourceType+"/"+h.ID
		}
		entries = append(entries, map[string]any{"resource": raw, "request": map[string]any{"method": method, "url": url}})
	}
	env, e := transactionEnvelope(entries)
	if e != nil {
		return ExtractionResult{}, e
	}
	return ExtractionResult{Bundle: env}, nil
}

func transactionEnvelope(entries []map[string]any) (*types.ResourceEnvelope, error) {
	b, e := json.Marshal(map[string]any{"resourceType": "Bundle", "type": "transaction", "entry": entries})
	if e != nil {
		return nil, e
	}
	return types.NewJSONCodec().ParseJSON("Bundle", b)
}

type StructureMapExtractor struct {
	Run func(context.Context, QuestionnaireResponse) ([]json.RawMessage, error)
}

func (s StructureMapExtractor) Extract(ctx context.Context, _ Questionnaire, r QuestionnaireResponse) (ExtractionResult, error) {
	if s.Run == nil {
		return ExtractionResult{}, fmt.Errorf("StructureMap runtime is unavailable")
	}
	return TemplateExtractor{Template: func(c context.Context, r QuestionnaireResponse) ([]json.RawMessage, error) { return s.Run(c, r) }}.Extract(ctx, Questionnaire{}, r)
}

type AdaptiveSession struct {
	ID            string
	Questionnaire string
	Response      QuestionnaireResponse
	State         map[string]any
}
type AdaptiveSearchRequest struct {
	Query   string
	Subject any
}
type AdaptiveEngine interface {
	Search(context.Context, AdaptiveSearchRequest) ([]Questionnaire, error)
	NextQuestion(context.Context, *AdaptiveSession) (*Item, error)
	SubmitAnswer(context.Context, *AdaptiveSession, ResponseItem) (*Item, error)
}

// SequentialAdaptiveEngine is a small deterministic adaptive engine for
// applications that do not need a policy-specific next-question service. It
// searches a supplied catalog, walks enabled question items in questionnaire
// order, and stores submitted answers in the session. Applications with
// branching, scoring, or remote policy can continue to inject their own
// AdaptiveEngine.
type SequentialAdaptiveEngine struct {
	Questionnaires []Questionnaire
	Resolver       QuestionnaireResolver
}

func (e SequentialAdaptiveEngine) Search(ctx context.Context, request AdaptiveSearchRequest) ([]Questionnaire, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(request.Query))
	var matches []Questionnaire
	for _, q := range e.Questionnaires {
		if query == "" || questionnaireMatches(q, query) {
			matches = append(matches, q)
		}
	}
	return matches, nil
}

func (e SequentialAdaptiveEngine) NextQuestion(ctx context.Context, session *AdaptiveSession) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q, err := e.sessionQuestionnaire(ctx, session)
	if err != nil {
		return nil, err
	}
	var next func([]Item) *Item
	next = func(items []Item) *Item {
		for _, item := range items {
			if !Enabled(item, session.Response) {
				continue
			}
			if item.Type != "group" && item.Type != "display" && findResponseDeep(session.Response.Item, item.LinkID) == nil {
				copy := item
				return &copy
			}
			if child := next(item.Item); child != nil {
				return child
			}
		}
		return nil
	}
	return next(q.Item), nil
}

func (e SequentialAdaptiveEngine) SubmitAnswer(ctx context.Context, session *AdaptiveSession, answer ResponseItem) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("adaptive session is nil")
	}
	if answer.LinkID == "" {
		return nil, fmt.Errorf("adaptive answer linkId is required")
	}
	q, err := e.sessionQuestionnaire(ctx, session)
	if err != nil {
		return nil, err
	}
	tree, err := Normalize(q)
	if err != nil {
		return nil, err
	}
	if len(tree.Resolve(answer.LinkID)) == 0 {
		return nil, fmt.Errorf("unknown adaptive answer linkId: %s", answer.LinkID)
	}
	if existing := findResponseDeep(session.Response.Item, answer.LinkID); existing != nil {
		*existing = answer
	} else {
		session.Response.Item = append(session.Response.Item, answer)
	}
	return e.NextQuestion(ctx, session)
}

func questionnaireMatches(q Questionnaire, query string) bool {
	if strings.Contains(strings.ToLower(q.ID), query) || strings.Contains(strings.ToLower(q.URL), query) || strings.Contains(strings.ToLower(q.Status), query) {
		return true
	}
	var walk func([]Item) bool
	walk = func(items []Item) bool {
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.LinkID), query) || strings.Contains(strings.ToLower(item.Text), query) || walk(item.Item) {
				return true
			}
		}
		return false
	}
	return walk(q.Item)
}

func (e SequentialAdaptiveEngine) sessionQuestionnaire(ctx context.Context, session *AdaptiveSession) (Questionnaire, error) {
	if session == nil {
		return Questionnaire{}, fmt.Errorf("adaptive session is nil")
	}
	if session.State != nil {
		switch value := session.State["questionnaire"].(type) {
		case Questionnaire:
			return value, nil
		case *Questionnaire:
			if value != nil {
				return *value, nil
			}
		}
	}
	if e.Resolver != nil {
		return e.Resolver.Resolve(ctx, session.Questionnaire)
	}
	for _, q := range e.Questionnaires {
		if Canonical(q) == session.Questionnaire || q.URL == session.Questionnaire {
			return q, nil
		}
	}
	return Questionnaire{}, fmt.Errorf("adaptive questionnaire not found: %s", session.Questionnaire)
}

type AdaptiveService struct{ Engine AdaptiveEngine }

func (s AdaptiveService) Search(ctx context.Context, r AdaptiveSearchRequest) ([]Questionnaire, error) {
	if s.Engine == nil {
		return nil, fmt.Errorf("adaptive engine is unavailable")
	}
	return s.Engine.Search(ctx, r)
}
func (s AdaptiveService) Next(ctx context.Context, x *AdaptiveSession) (*Item, error) {
	if s.Engine == nil {
		return nil, fmt.Errorf("adaptive engine is unavailable")
	}
	return s.Engine.NextQuestion(ctx, x)
}
func (s AdaptiveService) Submit(ctx context.Context, x *AdaptiveSession, a ResponseItem) (*Item, error) {
	if s.Engine == nil {
		return nil, fmt.Errorf("adaptive engine is unavailable")
	}
	return s.Engine.SubmitAnswer(ctx, x, a)
}
