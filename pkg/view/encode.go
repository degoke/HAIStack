package view

import (
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/shopspring/decimal"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RowEncoder normalizes fhirpath.Value collections into JSON-safe values.
// Empty results become null; singletons become scalars; multi-item results
// become arrays. Complex objects are converted only when they are already
// representable from pkg/fhirpath.Value (currently system.Quantity); otherwise
// ErrRowEncoding is returned.
type RowEncoder struct{}

// newRowEncoder returns an encoder.
func newRowEncoder() *RowEncoder {
	return &RowEncoder{}
}

// Encode converts a fhirpath collection to a JSON-safe value.
func (e *RowEncoder) Encode(values []fhirpath.Value) (any, error) {
	return e.EncodeColumn(values, false)
}

// EncodeColumn converts a FHIRPath collection to a JSON-safe value. When
// collection is true, singleton results are wrapped in a one-element array.
// When collection is false, multi-item results return ErrRowEncoding.
func (e *RowEncoder) EncodeColumn(values []fhirpath.Value, collection bool) (any, error) {
	if len(values) == 0 {
		if collection {
			return []any{}, nil
		}
		return nil, nil
	}
	if len(values) == 1 {
		val, err := e.encodeValue(values[0])
		if err != nil {
			return nil, err
		}
		if collection {
			if val == nil {
				return []any{}, nil
			}
			return []any{val}, nil
		}
		return val, nil
	}
	if !collection {
		return nil, fmt.Errorf("%w: FHIRPath returned %d values for scalar column; set collection: true or use .first()", ErrRowEncoding, len(values))
	}
	out := make([]any, len(values))
	for i, v := range values {
		val, err := e.encodeValue(v)
		if err != nil {
			return nil, err
		}
		out[i] = val
	}
	return out, nil
}

func (e *RowEncoder) encodeValue(v fhirpath.Value) (any, error) {
	if v.Raw() == nil {
		return nil, nil
	}
	if b, err := v.Bool(); err == nil {
		return b, nil
	}
	if s, err := v.String(); err == nil {
		return s, nil
	}
	if f, err := v.Float64(); err == nil {
		return f, nil
	}

	raw := v.Raw()
	switch val := raw.(type) {
	case system.Date:
		return val.String(), nil
	case system.Time:
		return val.String(), nil
	case system.DateTime:
		return val.String(), nil
	case system.Quantity:
		return quantityToMap(val), nil
	case string:
		return val, nil
	case bool:
		return val, nil
	case int:
		return val, nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint32:
		return int64(val), nil
	case uint64:
		return int64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case proto.Message:
		if scalar, ok := protoMessageToScalar(val); ok {
			return scalar, nil
		}
		return nil, fmt.Errorf("%w: unsupported proto message %s", ErrRowEncoding, val.ProtoReflect().Descriptor().FullName())
	default:
		return nil, fmt.Errorf("%w: unsupported value type %T", ErrRowEncoding, raw)
	}
}

func quantityToMap(q system.Quantity) map[string]any {
	pq := q.ToProtoQuantity()
	m := map[string]any{}
	if pq.Value != nil {
		d, err := decimal.NewFromString(pq.Value.Value)
		if err != nil {
			d = decimal.Zero
		}
		m["value"] = d.InexactFloat64()
	}
	if pq.Unit != nil {
		m["unit"] = pq.Unit.Value
	}
	if pq.System != nil {
		m["system"] = pq.System.Value
	}
	if pq.Code != nil {
		m["code"] = pq.Code.Value
	}
	return m
}

// protoWrapperToScalar extracts a scalar value from FHIR primitive wrapper
// messages. Most Google FHIR R4 primitive types are proto messages containing a
// scalar "value" field (and optional extension/id fields). For enum wrappers
// such as Patient_GenderCode, the enum value name is lowercased to match FHIR
// JSON code conventions.
func protoWrapperToScalar(msg proto.Message) (any, bool) {
	r := msg.ProtoReflect()
	desc := r.Descriptor()
	fields := desc.Fields()
	var valueField protoreflect.FieldDescriptor
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.Name() == protoreflect.Name("value") {
			valueField = f
			break
		}
	}
	if valueField == nil {
		return nil, false
	}
	if valueField.IsList() || valueField.IsMap() {
		return nil, false
	}

	v := r.Get(valueField)
	switch valueField.Kind() {
	case protoreflect.StringKind:
		return v.String(), true
	case protoreflect.BoolKind:
		return v.Bool(), true
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int64(v.Int()), true
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int(), true
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return int64(v.Uint()), true
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return int64(v.Uint()), true
	case protoreflect.FloatKind:
		return float64(v.Float()), true
	case protoreflect.DoubleKind:
		return v.Float(), true
	case protoreflect.EnumKind:
		valDesc := valueField.Enum().Values().ByNumber(v.Enum())
		if valDesc == nil {
			return nil, false
		}
		return strings.ToLower(string(valDesc.Name())), true
	default:
		return nil, false
	}
}
