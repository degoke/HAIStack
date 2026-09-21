package terminology

import (
	"context"
	"strings"
)

// SubsumesRequest asks whether BroadCode is an ancestor of NarrowCode in a code system.
type SubsumesRequest struct {
	ScopeID, System, Version, BroadCode, NarrowCode string
}

const maxSubsumptionDepth = 256

// Subsumes reports whether r.BroadCode subsumes r.NarrowCode using compiled parent links.
func (s *LocalService) Subsumes(ctx context.Context, r SubsumesRequest) (bool, error) {
	if s == nil || s.Store == nil {
		return false, nil
	}
	broad := strings.TrimSpace(r.BroadCode)
	narrow := strings.TrimSpace(r.NarrowCode)
	if broad == "" || narrow == "" || strings.TrimSpace(r.System) == "" {
		return false, nil
	}
	if strings.EqualFold(broad, narrow) {
		return true, nil
	}
	scope := s.effectiveScope(ctx, r.ScopeID)
	current := narrow
	for depth := 0; depth < maxSubsumptionDepth; depth++ {
		c, err := s.Store.LookupConcept(ctx, scope, r.System, r.Version, current)
		if err != nil {
			return false, err
		}
		if c == nil || strings.TrimSpace(c.ParentCode) == "" {
			return false, nil
		}
		parent := strings.TrimSpace(c.ParentCode)
		if strings.EqualFold(parent, broad) {
			return true, nil
		}
		current = parent
	}
	return false, nil
}

// Subsumes delegates to the first provider that implements SubsumptionService.
func (c Chain) Subsumes(ctx context.Context, r SubsumesRequest) (bool, error) {
	var last error
	for _, p := range c.Providers {
		sub, ok := p.(SubsumptionService)
		if !ok {
			continue
		}
		v, err := sub.Subsumes(ctx, r)
		if err != nil {
			last = err
			continue
		}
		if v {
			return true, nil
		}
	}
	if last != nil {
		return false, last
	}
	return false, nil
}
