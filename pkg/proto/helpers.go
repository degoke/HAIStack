package proto

import (
	"fmt"
	"reflect"

	"github.com/degoke/health-ai-stack/pkg/types"
	"google.golang.org/protobuf/proto"
)

var defaultGoogleR4Codec = NewGoogleR4Codec()

var (
	errNilProto               = fmt.Errorf("proto value is nil")
	errUnsupportedProto       = fmt.Errorf("unsupported Google FHIR R4 proto resource")
	errEmptyContainedResource = fmt.Errorf("contained resource has no resource set")
)

// IsProtoResource reports whether v is a supported Google FHIR R4 protobuf resource.
func IsProtoResource(v any) bool {
	if isNilValue(v) {
		return false
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return false
	}
	return isGoogleR4ProtoMessage(msg.ProtoReflect())
}

// ResourceTypeOfProto returns the FHIR resource type for a supported Google FHIR R4 proto value.
func ResourceTypeOfProto(v any) (string, error) {
	if isNilValue(v) {
		return "", errNilProto
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return "", errUnsupportedProto
	}
	return resourceTypeFromR4Message(msg.ProtoReflect())
}

// ToEnvelope converts a typed Google FHIR R4 resource to a canonical envelope.
// The original protobuf value is retained in the returned envelope's Proto field.
func ToEnvelope(resource any) (*types.ResourceEnvelope, error) {
	resourceType, err := ResourceType(resource)
	if err != nil {
		return nil, err
	}
	return defaultGoogleR4Codec.ProtoToEnvelope(resourceType, resource)
}

// ParseJSONToEnvelope parses FHIR JSON and returns a fully populated envelope with Proto attached.
func ParseJSONToEnvelope(resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	return defaultGoogleR4Codec.ParseJSONToEnvelope(resourceType, data)
}

// ToJSON converts a typed Google FHIR R4 resource to canonical FHIR JSON.
func ToJSON(resource any) ([]byte, error) {
	resourceType, err := ResourceType(resource)
	if err != nil {
		return nil, err
	}
	return defaultGoogleR4Codec.ProtoToJSON(resourceType, resource)
}

// ResourceType returns the FHIR resource type carried by a typed R4 resource.
func ResourceType(resource any) (string, error) {
	return ResourceTypeOfProto(resource)
}

func envelopeFromJSON(jsonCodec *types.JSONCodec, resourceType string, jsonBytes []byte, protoVal any) (*types.ResourceEnvelope, error) {
	envelope, err := jsonCodec.ParseJSON(resourceType, jsonBytes)
	if err != nil {
		return nil, err
	}
	envelope.Proto = protoVal
	return envelope, nil
}

func asProtoMessage(v any) (proto.Message, error) {
	if isNilValue(v) {
		return nil, errNilProto
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, errUnsupportedProto
	}
	if !isGoogleR4ProtoMessage(msg.ProtoReflect()) {
		return nil, errUnsupportedProto
	}
	return msg, nil
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func assertResourceTypeMatch(expected, actual string) error {
	if expected != "" && actual != expected {
		return fmt.Errorf("resourceType mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}
