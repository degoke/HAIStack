package sdc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func NewDraft(url string, items []Item) Questionnaire {
	return Questionnaire{ResourceType: "Questionnaire", URL: url, Status: "draft", Item: items}
}
func Publish(q Questionnaire) Questionnaire           { q.Status = "active"; return q }
func Version(q Questionnaire, v string) Questionnaire { q.Version = v; return q }
func Canonical(q Questionnaire) string {
	if q.Version == "" {
		return q.URL
	}
	return q.URL + "|" + q.Version
}
func DecodeQuestionnaire(raw []byte) (Questionnaire, error) {
	var q Questionnaire
	if err := json.Unmarshal(raw, &q); err != nil {
		return q, err
	}
	if q.ResourceType == "" {
		q.ResourceType = "Questionnaire"
	}
	return q, nil
}
func DecodeResponse(raw []byte) (QuestionnaireResponse, error) {
	var r QuestionnaireResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.ResourceType == "" {
		r.ResourceType = "QuestionnaireResponse"
	}
	return r, nil
}
func Envelope(resourceType, id string, v any) (*types.ResourceEnvelope, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return &types.ResourceEnvelope{ResourceType: resourceType, ID: id, JSON: b}, nil
}
func SaveQuestionnaire(ctx context.Context, s ResourceService, q Questionnaire) (*types.ResourceEnvelope, error) {
	if q.ResourceType == "" {
		q.ResourceType = "Questionnaire"
	}
	e, err := Envelope("Questionnaire", q.ID, q)
	if err != nil {
		return nil, err
	}
	if q.ID == "" {
		return s.Create(ctx, e)
	}
	return s.Update(ctx, e)
}

// SaveQuestionnaireResource persists an already-canonical Questionnaire using
// the existing core resource service lifecycle.
func SaveQuestionnaireResource(ctx context.Context, s ResourceService, env *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if err := requireResource(env, "Questionnaire"); err != nil {
		return nil, err
	}
	if env.ID == "" {
		return s.Create(ctx, env)
	}
	return s.Update(ctx, env)
}
func SaveResponse(ctx context.Context, s ResourceService, r QuestionnaireResponse) (*types.ResourceEnvelope, error) {
	e, err := Envelope("QuestionnaireResponse", r.ID, r)
	if err != nil {
		return nil, err
	}
	if r.ID == "" {
		return s.Create(ctx, e)
	}
	return s.Update(ctx, e)
}

func SaveQuestionnaireResponseResource(ctx context.Context, s ResourceService, env *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if err := requireResource(env, "QuestionnaireResponse"); err != nil {
		return nil, err
	}
	if env.ID == "" {
		return s.Create(ctx, env)
	}
	return s.Update(ctx, env)
}

type CanonicalResolver struct {
	Resources ResourceService
	Finder    QuestionnaireFinder
}

type QuestionnaireFinder interface {
	FindQuestionnaire(context.Context, string) (Questionnaire, error)
}
type QuestionnaireFinderFunc func(context.Context, string) (Questionnaire, error)

func (f QuestionnaireFinderFunc) FindQuestionnaire(ctx context.Context, c string) (Questionnaire, error) {
	return f(ctx, c)
}

// StoreQuestionnaireResolver resolves canonical questionnaires from the
// repository's existing ResourceStore without introducing SDC-specific tables.
type StoreQuestionnaireResolver struct{ Resources store.ResourceStore }

func (s StoreQuestionnaireResolver) Resolve(ctx context.Context, canonical string) (Questionnaire, error) {
	return s.FindQuestionnaire(ctx, canonical)
}

func (s StoreQuestionnaireResolver) FindQuestionnaire(ctx context.Context, canonical string) (Questionnaire, error) {
	if s.Resources == nil {
		return Questionnaire{}, fmt.Errorf("resource store is unavailable")
	}
	ids, err := s.Resources.ListIDs(ctx, "Questionnaire", 10000, 0)
	if err != nil {
		return Questionnaire{}, err
	}
	for _, id := range ids {
		env, err := s.Resources.Read(ctx, "Questionnaire", id)
		if err != nil {
			continue
		}
		q, err := DecodeQuestionnaire(env.JSON)
		if err != nil {
			continue
		}
		if Canonical(q) == canonical || q.URL == canonical {
			return q, nil
		}
	}
	return Questionnaire{}, fmt.Errorf("questionnaire canonical not found: %s", canonical)
}

func (c CanonicalResolver) Resolve(ctx context.Context, canonical string) (Questionnaire, error) {
	if c.Finder != nil {
		return c.Finder.FindQuestionnaire(ctx, canonical)
	}
	if f, ok := any(c.Resources).(QuestionnaireFinder); ok {
		return f.FindQuestionnaire(ctx, canonical)
	}
	if c.Resources == nil {
		return Questionnaire{}, fmt.Errorf("questionnaire resource service is unavailable")
	}
	return Questionnaire{}, fmt.Errorf("canonical resolution requires a search adapter for %q", canonical)
}
