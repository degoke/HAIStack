package sdc

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

var _ types.ClientValidationOutcomeError = ValidationError{}

type stubQuestionnaireResolver struct {
	q     Questionnaire
	err   error
	canon string
}

func (s stubQuestionnaireResolver) Resolve(_ context.Context, canonical string) (Questionnaire, error) {
	s.canon = canonical
	if s.err != nil {
		return Questionnaire{}, s.err
	}
	return s.q, nil
}

func TestResponseValidatorReturnsValidationErrorWithAllIssues(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "a", Type: "string", Required: true},
		{LinkID: "b", Type: "integer"},
	})
	resolver := stubQuestionnaireResolver{q: q}
	v := &ResponseValidator{Resolver: resolver}
	env, err := ResponseProjectionEnvelope(QuestionnaireResponse{
		ResourceType:  "QuestionnaireResponse",
		Questionnaire: q.URL,
		Status:        "in-progress",
		Item: []ResponseItem{
			{LinkID: "a"},
			{LinkID: "b", Answer: []Answer{{Value: "not-int"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = v.ValidateResource(context.Background(), env)
	if err == nil {
		t.Fatal("expected validation error")
	}
	outcome, ok := OutcomeFromError(err)
	if !ok || len(outcome.Issue) < 2 {
		t.Fatalf("outcome = %+v, ok=%v", outcome, ok)
	}
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("kind = %v, want invalid", core.KindOf(err))
	}
}

func TestResponseValidatorQuestionnaireNotFoundIsNotValidationError(t *testing.T) {
	v := &ResponseValidator{
		Resolver: stubQuestionnaireResolver{err: types.ErrQuestionnaireNotFound},
	}
	env, err := ResponseProjectionEnvelope(QuestionnaireResponse{
		ResourceType:  "QuestionnaireResponse",
		Questionnaire: "http://example/missing",
		Status:        "in-progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = v.ValidateResource(context.Background(), env)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := OutcomeFromError(err); ok {
		t.Fatal("not-found should not be ValidationError")
	}
	if core.KindOf(err) != core.ErrorKindNotFound {
		t.Fatalf("kind = %v, want not-found", core.KindOf(err))
	}
	if !errors.Is(err, types.ErrQuestionnaireNotFound) {
		t.Fatal("expected ErrQuestionnaireNotFound in chain")
	}
}

func TestResponseValidatorResolverRequiredIsInvalid(t *testing.T) {
	v := &ResponseValidator{}
	env, err := ResponseProjectionEnvelope(QuestionnaireResponse{
		ResourceType:  "QuestionnaireResponse",
		Questionnaire: "http://example/q",
		Status:        "in-progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = v.ValidateResource(context.Background(), env)
	if err == nil {
		t.Fatal("expected error")
	}
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("kind = %v, want invalid", core.KindOf(err))
	}
}

func TestDefaultSaveResponseOptionsUsesResourceStore(t *testing.T) {
	store := &memResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{}}
	svc := &stubResourceService{store: store}
	opts := DefaultSaveResponseOptions(svc)
	if opts.Resolver == nil {
		t.Fatal("expected resolver when resource store is available")
	}
}

func TestSaveQuestionnaireResponseResourceValidatesWithDefaultResolver(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "a", Type: "string", Required: true}})
	qenv, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	qenv.ID = "q1"
	renv, err := ResponseProjectionEnvelope(QuestionnaireResponse{
		ResourceType:  "QuestionnaireResponse",
		Questionnaire: q.URL,
		Status:        "in-progress",
		Item:          []ResponseItem{{LinkID: "a"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := &memResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Questionnaire": {"q1": qenv},
	}}
	svc := &stubResourceService{store: store}
	_, err = SaveQuestionnaireResponseResource(context.Background(), svc, renv)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if _, ok := OutcomeFromError(err); !ok {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

type memResourceStore struct {
	byType map[string]map[string]*types.ResourceEnvelope
}

func (m *memResourceStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (m *memResourceStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (m *memResourceStore) Delete(context.Context, string, string) error          { return nil }
func (m *memResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	_, ok := m.byType[resourceType][id]
	return ok, nil
}
func (m *memResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env, ok := m.byType[resourceType][id]; ok {
		return env, nil
	}
	return nil, errors.New("resource not found")
}
func (m *memResourceStore) ListIDs(_ context.Context, resourceType string, _, _ int) ([]string, error) {
	byID := m.byType[resourceType]
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	return ids, nil
}

type stubResourceService struct {
	store store.ResourceStore
}

func (s *stubResourceService) ResourceStore() store.ResourceStore {
	return s.store
}

func (s *stubResourceService) Create(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, errors.New("create should not be called")
}

func (s *stubResourceService) Read(ctx context.Context, typ, id string) (*types.ResourceEnvelope, error) {
	return s.store.Read(ctx, typ, id)
}

func (s *stubResourceService) Update(context.Context, *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return nil, errors.New("update should not be called")
}
