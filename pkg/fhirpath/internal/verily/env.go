package verily

import (
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/shopspring/decimal"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
)

// EnvValueFromAny converts a Go value into a FHIRPath environment constant.
func EnvValueFromAny(value any, codec proto.ProtoCodec) (any, error) {
	if value == nil {
		return system.Collection{}, nil
	}
	switch v := value.(type) {
	case system.Any:
		return v, nil
	case verilyfhirpath.Resource:
		return v, nil
	case *types.ResourceEnvelope:
		return ResourceFromInput(v, codec)
	case map[string]any:
		if rt, ok := v["resourceType"].(string); ok && rt != "" {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			env, err := types.NewJSONCodec().ParseJSON(rt, b)
			if err != nil {
				return nil, err
			}
			return ResourceFromInput(env, codec)
		}
		return nil, fmt.Errorf("%w: map without resourceType", ErrInvalidInput)
	default:
		if proto.IsProtoResource(value) {
			return unwrapProtoResource(value)
		}
	}
	switch value {
	case true:
		return system.Boolean(true), nil
	case false:
		return system.Boolean(false), nil
	}
	switch x := value.(type) {
	case string:
		return system.String(x), nil
	case int:
		return system.Integer(x), nil
	case int32:
		return system.Integer(int(x)), nil
	case int64:
		return system.Integer(int(x)), nil
	case float32:
		return system.Decimal(decimal.NewFromFloat(float64(x))), nil
	case float64:
		return system.Decimal(decimal.NewFromFloat(x)), nil
	}
	return nil, fmt.Errorf("%w: unsupported environment constant type %T", ErrInvalidInput, value)
}
