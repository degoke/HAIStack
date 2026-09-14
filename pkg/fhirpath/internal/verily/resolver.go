package verily

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/proto"
	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
)

// ResolveFunc resolves one FHIR reference string to a resource input accepted by
// ResourceFromInput.
type ResolveFunc func(ctx context.Context, ref string) (any, error)

type contextResolver struct {
	ctx   context.Context
	fn    ResolveFunc
	codec proto.ProtoCodec
}

func (r *contextResolver) Resolve(input []string) ([]verilyfhirpath.Resource, error) {
	if r.fn == nil {
		return nil, ErrUnconfiguredResolver
	}
	out := make([]verilyfhirpath.Resource, 0, len(input))
	for _, ref := range input {
		if ref == "" {
			continue
		}
		resolved, err := r.fn(r.ctx, ref)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			continue
		}
		resource, err := ResourceFromInput(resolved, r.codec)
		if err != nil {
			continue
		}
		out = append(out, resource)
	}
	return out, nil
}
