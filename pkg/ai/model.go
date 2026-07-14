package ai

import "context"

// ModelRequest carries a model invocation request. Tool execution does not
// require a model adapter; this seam is for optional local/cloud routing.
type ModelRequest struct {
	Hint    string
	Prompt  string
	Context string
	Tools   []string
}

// ModelResponse is the outcome of a model invocation.
type ModelResponse struct {
	Adapter string
	Content string
}

// ModelAdapter invokes a language model backend.
type ModelAdapter interface {
	Name() string
	Invoke(ctx context.Context, req ModelRequest) (*ModelResponse, error)
}

// ModelRouter selects a local or cloud model adapter based on a hint.
type ModelRouter struct {
	Local ModelAdapter
	Cloud ModelAdapter
}

// Route returns the adapter for hint. "cloud" selects Cloud when configured;
// everything else selects Local when configured, then Cloud as fallback.
func (r *ModelRouter) Route(hint string) ModelAdapter {
	if r == nil {
		return nil
	}
	if hint == "cloud" && r.Cloud != nil {
		return r.Cloud
	}
	if r.Local != nil {
		return r.Local
	}
	return r.Cloud
}

// Invoke routes and invokes a model when an adapter is configured.
func (r *ModelRouter) Invoke(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	adapter := r.Route(req.Hint)
	if adapter == nil {
		return nil, nil
	}
	resp, err := adapter.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Adapter == "" {
		resp.Adapter = adapter.Name()
	}
	return resp, nil
}
