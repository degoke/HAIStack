package terminology

// ChainInvalidator forwards cache invalidation to every provider in a chain.
type ChainInvalidator struct {
	Providers []Invalidator
}

func (c ChainInvalidator) InvalidateCodeSystem(system, version string) {
	for _, p := range c.Providers {
		if p != nil {
			p.InvalidateCodeSystem(system, version)
		}
	}
}

func (c ChainInvalidator) InvalidateValueSet(url, version string) {
	for _, p := range c.Providers {
		if p != nil {
			p.InvalidateValueSet(url, version)
		}
	}
}
