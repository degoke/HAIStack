package search_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type projectionExecutor struct {
	result *search.ExecuteResult
}

func (e projectionExecutor) Execute(context.Context, *search.Plan) (*search.ExecuteResult, error) {
	return e.result, nil
}

type projectionResourceStore struct {
	resources map[string]*types.ResourceEnvelope
}

func (s projectionResourceStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (s projectionResourceStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (s projectionResourceStore) Delete(context.Context, string, string) error          { return nil }
func (s projectionResourceStore) Exists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s projectionResourceStore) ListIDs(context.Context, string, int, int) ([]string, error) {
	return nil, nil
}

func (s projectionResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	res := s.resources[resourceType+"/"+id]
	if res == nil {
		return nil, errors.New("not found")
	}
	copy := *res
	return &copy, nil
}

func TestSearchProjectionFailureReturnsError(t *testing.T) {
	svc, err := search.NewService(search.ServiceConfig{
		Registry: search.NewSnapshotRegistry(testSnapshot(t, "Patient")),
		Executor: projectionExecutor{result: &search.ExecuteResult{IDs: []string{"pat-1"}}},
		Resources: projectionResourceStore{resources: map[string]*types.ResourceEnvelope{
			"Patient/pat-1": {
				ResourceType: "Patient",
				ID:           "pat-1",
				JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"`),
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc.Search(context.Background(), "Patient", url.Values{"_summary": {"true"}})
	if !errors.Is(err, search.ErrProjectionFailed) {
		t.Fatalf("err = %v, want ErrProjectionFailed", err)
	}
}

var _ store.ResourceStore = projectionResourceStore{}
