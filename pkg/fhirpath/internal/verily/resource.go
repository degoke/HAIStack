package verily

import (
	"fmt"

	"github.com/degoke/haistack/pkg/proto"
	"github.com/degoke/haistack/pkg/types"
	rpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/bundle_and_contained_resource_go_proto"
	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
	protobuf "google.golang.org/protobuf/proto"
)

const r4ContainedResourceOneof = "oneof_resource"

// ResourceFromInput adapts supported haistack evaluation inputs to a Verily FHIR resource.
func ResourceFromInput(resource any, codec proto.ProtoCodec) (verilyfhirpath.Resource, error) {
	switch v := resource.(type) {
	case *types.ResourceEnvelope:
		return resourceFromEnvelope(v, codec)
	default:
		if !proto.IsProtoResource(resource) {
			return nil, fmt.Errorf("%w: unsupported type %T", ErrInvalidInput, resource)
		}
		return unwrapProtoResource(resource)
	}
}

func resourceFromEnvelope(envelope *types.ResourceEnvelope, codec proto.ProtoCodec) (verilyfhirpath.Resource, error) {
	if envelope == nil {
		return nil, fmt.Errorf("%w: nil ResourceEnvelope", ErrInvalidInput)
	}
	if envelope.Proto != nil {
		return unwrapProtoResource(envelope.Proto)
	}
	if len(envelope.JSON) == 0 {
		return nil, fmt.Errorf("%w: envelope has no proto or JSON", ErrInvalidInput)
	}
	pb, err := codec.ParseJSONToProto(envelope.ResourceType, envelope.JSON)
	if err != nil {
		return nil, err
	}
	return unwrapProtoResource(pb)
}

func unwrapProtoResource(v any) (verilyfhirpath.Resource, error) {
	msg, ok := v.(protobuf.Message)
	if !ok || !proto.IsProtoResource(v) {
		return nil, fmt.Errorf("%w: unsupported proto value", ErrInvalidInput)
	}
	if cr, ok := msg.(*rpb.ContainedResource); ok {
		return unwrapContainedResource(cr)
	}
	resource, ok := msg.(verilyfhirpath.Resource)
	if !ok {
		return nil, fmt.Errorf("%w: proto does not implement FHIR resource", ErrInvalidInput)
	}
	return resource, nil
}

func unwrapContainedResource(cr *rpb.ContainedResource) (verilyfhirpath.Resource, error) {
	if cr == nil {
		return nil, fmt.Errorf("%w: nil ContainedResource", ErrInvalidInput)
	}
	msg := cr.ProtoReflect()
	od := msg.Descriptor().Oneofs().ByName(r4ContainedResourceOneof)
	if od == nil {
		return nil, fmt.Errorf("%w: invalid ContainedResource", ErrInvalidInput)
	}
	field := msg.WhichOneof(od)
	if field == nil {
		return nil, fmt.Errorf("%w: empty ContainedResource", ErrInvalidInput)
	}
	inner := msg.Get(field).Message().Interface()
	resource, ok := inner.(verilyfhirpath.Resource)
	if !ok {
		return nil, fmt.Errorf("%w: contained resource is not a FHIR resource", ErrInvalidInput)
	}
	return resource, nil
}
