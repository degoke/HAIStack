package conceptmap

import "context"

// ChainResolver tries resolvers in order until one succeeds.
type ChainResolver struct {
	Resolvers []Resolver
}

func (c ChainResolver) Resolve(ctx context.Context, canonical string) (Map, error) {
	var lastErr error
	for _, resolver := range c.Resolvers {
		if resolver == nil {
			continue
		}
		m, err := resolver.Resolve(ctx, canonical)
		if err == nil {
			return m, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return Map{}, lastErr
	}
	return Map{}, ErrNotFound(canonical)
}

// ErrNotFound reports a missing ConceptMap canonical URL.
func ErrNotFound(canonical string) error {
	return &notFoundError{canonical: canonical}
}

type notFoundError struct {
	canonical string
}

func (e *notFoundError) Error() string {
	return "ConceptMap not found: " + e.canonical
}
