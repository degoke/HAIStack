package sdc

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// TerminologyAdapter adapts the terminology service to SDC validation.
type TerminologyAdapter struct {
	Service terminology.Provider
	ScopeID string
}

func (a TerminologyAdapter) scopeID(ctx context.Context) string {
	if scope := store.TerminologyScopeFromContext(ctx); scope != "" {
		return scope
	}
	return a.ScopeID
}

func (a TerminologyAdapter) ValidateCode(ctx context.Context, c Coding, valueSet string) error {
	if a.Service == nil || valueSet == "" {
		return nil
	}
	result, err := a.Service.ValidateCode(ctx, terminology.ValidateCodeRequest{
		ScopeID: a.scopeID(ctx),
		URL:     valueSet,
		Coding:  terminology.Coding{System: c.System, Code: c.Code, Display: c.Display},
	})
	if err != nil {
		return err
	}
	if result != nil && result.Status == terminology.Valid {
		return nil
	}
	if result != nil && result.Message != "" {
		return fmt.Errorf("%s", result.Message)
	}
	return fmt.Errorf("code %s|%s is not in value set %s", c.System, c.Code, valueSet)
}

func (a TerminologyAdapter) Display(_ context.Context, c Coding) (string, error) {
	if c.Display != "" {
		return c.Display, nil
	}
	return c.Code, nil
}

func (a TerminologyAdapter) Expand(ctx context.Context, url string) ([]Coding, error) {
	if a.Service == nil {
		return nil, fmt.Errorf("value set expansion unavailable: %s", url)
	}
	expanded, err := a.Service.Expand(ctx, terminology.ExpandRequest{ScopeID: a.scopeID(ctx), URL: url})
	if err != nil {
		return nil, err
	}
	out := make([]Coding, 0, len(expanded.Contains))
	for _, c := range expanded.Contains {
		out = append(out, Coding{System: c.System, Code: c.Code, Display: c.Display})
	}
	return out, nil
}
