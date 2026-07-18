package terminology

import "context"

// Chain resolves terminology providers in precedence order. A provider that
// cannot answer leaves resolution to the next provider; exact-version requests
// are never rewritten by the chain.
type Chain struct{ Providers []Provider }

func (c Chain) Lookup(ctx context.Context, r LookupRequest) (*LookupResult, error) {
	var last error
	for _, p := range c.Providers {
		v, e := p.Lookup(ctx, r)
		if e != nil {
			last = e
			continue
		}
		if v != nil && v.Found {
			return v, nil
		}
	}
	if last != nil {
		return nil, last
	}
	return &LookupResult{}, nil
}
func (c Chain) Expand(ctx context.Context, r ExpandRequest) (*Expansion, error) {
	var last error
	for _, p := range c.Providers {
		v, e := p.Expand(ctx, r)
		if e != nil {
			last = e
			continue
		}
		if v != nil {
			return v, nil
		}
	}
	return nil, last
}
func (c Chain) ValidateCode(ctx context.Context, r ValidateCodeRequest) (*ValidationResult, error) {
	var last error
	for _, p := range c.Providers {
		v, e := p.ValidateCode(ctx, r)
		if e != nil {
			last = e
			continue
		}
		if v == nil {
			continue
		}
		if v.Status != UnknownTerminology {
			return v, nil
		}
	}
	if last != nil {
		return nil, last
	}
	return &ValidationResult{Status: UnknownTerminology, Message: "terminology is not known"}, nil
}
