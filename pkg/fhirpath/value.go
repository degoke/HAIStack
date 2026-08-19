package fhirpath

import (
	"fmt"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	"github.com/shopspring/decimal"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
	"google.golang.org/protobuf/proto"
)

// Value wraps one FHIRPath result item. Backend-native system types remain
// accessible through Raw() but are not part of the public type surface.
type Value struct {
	raw any
}

// NewValue wraps a backend result item for the public API.
func NewValue(raw any) Value {
	return Value{raw: raw}
}

// Type returns a stable type name for the wrapped value.
func (v Value) Type() string {
	if v.raw == nil {
		return "null"
	}
	switch v.raw.(type) {
	case system.Boolean:
		return "Boolean"
	case system.String:
		return "String"
	case system.Integer:
		return "Integer"
	case system.Decimal:
		return "Decimal"
	case system.Date:
		return "Date"
	case system.Time:
		return "Time"
	case system.DateTime:
		return "DateTime"
	case system.Quantity:
		return "Quantity"
	case *dtpb.Boolean:
		return "Boolean"
	case *dtpb.String, *dtpb.Uri, *dtpb.Url, *dtpb.Code, *dtpb.Oid, *dtpb.Id, *dtpb.Uuid, *dtpb.Markdown, *dtpb.Canonical:
		return "String"
	case *dtpb.Integer, *dtpb.PositiveInt, *dtpb.UnsignedInt:
		return "Integer"
	case *dtpb.Decimal:
		return "Decimal"
	case proto.Message:
		return string(v.raw.(proto.Message).ProtoReflect().Descriptor().Name())
	default:
		return fmt.Sprintf("%T", v.raw)
	}
}

// Raw returns the backend-native value.
func (v Value) Raw() any {
	return v.raw
}

// Bool coerces a singleton boolean result.
func (v Value) Bool() (bool, error) {
	switch val := v.raw.(type) {
	case system.Boolean:
		return bool(val), nil
	case *dtpb.Boolean:
		return val.GetValue(), nil
	default:
		return false, ErrTypeMismatch
	}
}

// String coerces a singleton string result.
func (v Value) String() (string, error) {
	switch val := v.raw.(type) {
	case system.String:
		return string(val), nil
	case system.Date:
		return val.String(), nil
	case system.DateTime:
		return val.String(), nil
	case system.Time:
		return val.String(), nil
	case system.Quantity:
		return val.String(), nil
	case *dtpb.String:
		return val.GetValue(), nil
	case *dtpb.Uri:
		return val.GetValue(), nil
	case *dtpb.Url:
		return val.GetValue(), nil
	case *dtpb.Code:
		return val.GetValue(), nil
	case *dtpb.Id:
		return val.GetValue(), nil
	case *dtpb.Markdown:
		return val.GetValue(), nil
	case *dtpb.Canonical:
		return val.GetValue(), nil
	case *dtpb.Date:
		value, err := system.DateFromProto(val)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case *dtpb.DateTime:
		value, err := system.DateTimeFromProto(val)
		if err != nil {
			return "", err
		}
		return value.String(), nil
	case *dtpb.Time:
		return system.TimeFromProto(val).String(), nil
	default:
		if s, ok := val.(string); ok {
			return s, nil
		}
		return "", ErrTypeMismatch
	}
}

// Float64 coerces a singleton numeric result.
func (v Value) Float64() (float64, error) {
	switch val := v.raw.(type) {
	case system.Integer:
		return float64(val), nil
	case system.Decimal:
		return decimal.Decimal(val).InexactFloat64(), nil
	case *dtpb.Integer:
		return float64(val.GetValue()), nil
	case *dtpb.PositiveInt:
		return float64(val.GetValue()), nil
	case *dtpb.UnsignedInt:
		return float64(val.GetValue()), nil
	case *dtpb.Decimal:
		d, err := decimal.NewFromString(val.GetValue())
		if err != nil {
			return 0, err
		}
		return d.InexactFloat64(), nil
	default:
		return 0, ErrTypeMismatch
	}
}

func valuesFromBackend(items []any) []Value {
	if len(items) == 0 {
		return nil
	}
	out := make([]Value, len(items))
	for i, item := range items {
		out[i] = NewValue(item)
	}
	return out
}

func backendFromCollection(c Collection) []any {
	if len(c) == 0 {
		return nil
	}
	out := make([]any, len(c))
	for i, v := range c {
		out[i] = v.raw
	}
	return out
}
