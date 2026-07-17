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

func (a Assembler) Assemble(ctx context.Context, q Questionnaire) (Questionnaire, Outcome) {
	o := ValidateQuestionnaire(q, ValidationOptions{})
	if len(o.Issue) > 0 {
		return q, o
	}
	out := q
	var expand func([]Item) []Item
	expand = func(items []Item) []Item {
		var r []Item
		for _, it := range items {
			if strings.HasPrefix(it.Definition, "http") && a.Resolver != nil {
				ref, e := a.Resolver.Resolve(ctx, it.Definition)
				if e != nil {
					o.add("error", "not-found", e.Error(), it.LinkID)
					r = append(r, it)
					continue
				}
				r = append(r, ref.Item...)
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
	for _, m := range mappings {
		ri := findResponse(r.Item, m.LinkID)
		if ri == nil || len(ri.Answer) == 0 {
			continue
		}
		for n, answer := range ri.Answer {
			res := map[string]any{"resourceType": m.ResourceType}
			if m.Identity != "" {
				identity := strings.TrimPrefix(m.Identity, m.ResourceType+"/")
				if identity != "" {
					res["id"] = identity
				}
			}
			if m.Path != "" {
				setPath(res, m.Path, answer.Value)
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
			if n > 0 {
				full = fmt.Sprintf("%s-%d", full, n)
			}
			entries = append(entries, map[string]any{"fullUrl": full, "resource": json.RawMessage(b), "request": map[string]any{"method": method, "url": url}})
		}
	}
	env, err := transactionEnvelope(entries)
	if err != nil {
		return ExtractionResult{}, err
	}
	return ExtractionResult{Bundle: env}, nil
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
