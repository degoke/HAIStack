package verily

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/proto"
	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
	"github.com/verily-src/fhirpath-go/fhirpath/evalopts"
)

// EvalOptions builds Verily evaluate options for env constants, resolve(), and memberOf().
func EvalOptions(ctx context.Context, env map[string]any, codec proto.ProtoCodec, resolve ResolveFunc, terminology TerminologyValidator) ([]verilyfhirpath.EvaluateOption, error) {
	opts, err := EvalOptionsFromEnv(env, codec)
	if err != nil {
		return nil, err
	}
	if resolve != nil {
		opts = append(opts, evalopts.WithResolver(&contextResolver{
			ctx:   ctx,
			fn:    resolve,
			codec: codec,
		}))
	}
	if terminology != nil {
		opts = append(opts, evalopts.WithTerminologyService(&terminologyAdapter{validator: terminology}))
	}
	return opts, nil
}
