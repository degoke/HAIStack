package terminology

import "context"

// ChainInvalidator forwards cache invalidation to every provider in a chain.
type ChainInvalidator struct {
	Providers []Invalidator
}

func (c ChainInvalidator) InvalidateCodeSystem(ctx context.Context, system, version string) {
	for _, p := range c.Providers {
		if p != nil {
			p.InvalidateCodeSystem(ctx, system, version)
		}
	}
}

func (c ChainInvalidator) InvalidateValueSet(ctx context.Context, url, version string) {
	for _, p := range c.Providers {
		if p != nil {
			p.InvalidateValueSet(ctx, url, version)
		}
	}
}
