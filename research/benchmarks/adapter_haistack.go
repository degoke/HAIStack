package benchmarks

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/degoke/health-ai-stack/research/internal/memstore"
)

// HAIStackAdapter runs workloads against in-process HAIStack libraries.
type HAIStackAdapter struct {
	resources *memstore.ResourceStore
	views     *view.Executor
	ai        *ai.Executor
}

// Name implements Adapter.
func (a *HAIStackAdapter) Name() string { return "haistack" }

// NewHAIStackAdapter constructs an in-memory HAIStack adapter.
func NewHAIStackAdapter() (*HAIStackAdapter, error) {
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return nil, err
	}
	resources := memstore.New()
	reg := view.NewRegistry()
	if _, err := reg.Register(view.ObservationView(), fp); err != nil {
		return nil, err
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    fp,
		Registry:  reg,
	})
	if err != nil {
		return nil, err
	}
	policy := ai.NewAllowListPolicy()
	policy.Views["observation_view"] = ai.ViewTypePolicy{MaxCount: 500}
	aiExec, err := ai.NewExecutor(ai.Config{
		Resources: resources,
		Views:     exec,
		Policy:    policy,
	})
	if err != nil {
		return nil, err
	}
	return &HAIStackAdapter{resources: resources, views: exec, ai: aiExec}, nil
}

// Load implements Adapter.
func (a *HAIStackAdapter) Load(ctx context.Context, resources []*types.ResourceEnvelope) error {
	for _, res := range resources {
		if err := a.resources.Create(ctx, res); err != nil {
			return err
		}
	}
	return nil
}

// Read implements Adapter.
func (a *HAIStackAdapter) Read(ctx context.Context, resourceType, id string) error {
	_, err := a.resources.Read(ctx, resourceType, id)
	return err
}

// RunView implements Adapter.
func (a *HAIStackAdapter) RunView(ctx context.Context, name, version string) (int, error) {
	result, err := a.views.Execute(ctx, view.ExecuteRequest{ViewName: name, Version: version})
	if err != nil {
		return 0, err
	}
	return len(result.Rows), nil
}

// RunAIView implements Adapter.
func (a *HAIStackAdapter) RunAIView(ctx context.Context, name, version string) (int, error) {
	res, err := a.ai.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolRunView,
		Actor:    "benchmark",
		Input:    map[string]any{"viewName": name, "version": version},
	})
	if err != nil {
		return 0, err
	}
	data, _ := res.Data.(map[string]any)
	switch rows := data["rows"].(type) {
	case []map[string]any:
		return len(rows), nil
	case []any:
		return len(rows), nil
	default:
		return 0, fmt.Errorf("unexpected view data %T", res.Data)
	}
}
