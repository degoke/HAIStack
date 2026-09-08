package verily

import (
	"github.com/degoke/health-ai-stack/pkg/proto"
	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
	"github.com/verily-src/fhirpath-go/fhirpath/evalopts"
)

// EvalOptionsFromEnv builds verily evaluate options for SDC-style % constants.
func EvalOptionsFromEnv(env map[string]any, codec proto.ProtoCodec) ([]verilyfhirpath.EvaluateOption, error) {
	if len(env) == 0 {
		return nil, nil
	}
	opts := make([]verilyfhirpath.EvaluateOption, 0, len(env))
	for name, value := range env {
		converted, err := EnvValueFromAny(value, codec)
		if err != nil {
			return nil, err
		}
		opts = append(opts, evalopts.EnvVariable(name, converted))
	}
	return opts, nil
}
