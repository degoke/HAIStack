package proto

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/fhir/go/fhirversion"
	"github.com/google/fhir/go/jsonformat"
	rpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/bundle_and_contained_resource_go_proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const googleR4Timezone = "UTC"

// GoogleR4Codec implements ProtoCodec using Google FHIR Go R4 protobufs.
type GoogleR4Codec struct {
	unmarshaller *jsonformat.Unmarshaller
	marshaller   *jsonformat.Marshaller
	jsonCodec    *types.JSONCodec
}

var _ ProtoCodec = (*GoogleR4Codec)(nil)

// NewGoogleR4Codec returns a GoogleR4Codec ready for use.
func NewGoogleR4Codec() *GoogleR4Codec {
	unmarshaller, err := jsonformat.NewUnmarshaller(googleR4Timezone, fhirversion.R4)
	if err != nil {
		panic(fmt.Sprintf("proto: init Google R4 unmarshaller: %v", err))
	}
	marshaller, err := jsonformat.NewMarshaller(false, "", "", fhirversion.R4)
	if err != nil {
		panic(fmt.Sprintf("proto: init Google R4 marshaller: %v", err))
	}
	return &GoogleR4Codec{
		unmarshaller: unmarshaller,
		marshaller:   marshaller,
		jsonCodec:    types.NewJSONCodec(),
	}
}

// ParseJSONToEnvelope parses FHIR JSON into proto and returns a fully populated envelope.
func (c *GoogleR4Codec) ParseJSONToEnvelope(resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	pb, err := c.ParseJSONToProto(resourceType, data)
	if err != nil {
		return nil, err
	}
	rt := resourceType
	if rt == "" {
		rt, err = ResourceTypeOfProto(pb)
		if err != nil {
			return nil, err
		}
	}
	return c.ProtoToEnvelope(rt, pb)
}

// ParseJSONToProto validates resourceType and parses FHIR JSON into a Google R4 ContainedResource.
func (c *GoogleR4Codec) ParseJSONToProto(resourceType string, data []byte) (any, error) {
	payloadType, err := types.GetResourceType(data)
	if err != nil {
		return nil, err
	}
	if err := assertResourceTypeMatch(resourceType, payloadType); err != nil {
		return nil, err
	}

	cr, err := c.unmarshaller.UnmarshalR4(data)
	if err != nil {
		return nil, err
	}
	return cr, nil
}

// ProtoToJSON converts a supported Google R4 proto resource to canonical FHIR JSON.
func (c *GoogleR4Codec) ProtoToJSON(resourceType string, protoVal any) ([]byte, error) {
	msg, err := asProtoMessage(protoVal)
	if err != nil {
		return nil, err
	}

	actualType, err := ResourceTypeOfProto(msg)
	if err != nil {
		return nil, err
	}
	if err := assertResourceTypeMatch(resourceType, actualType); err != nil {
		return nil, err
	}

	var jsonBytes []byte
	switch msg.ProtoReflect().Descriptor().FullName() {
	case containedResourceFullName():
		jsonBytes, err = c.marshaller.Marshal(msg)
	default:
		jsonBytes, err = c.marshaller.MarshalResource(msg)
	}
	if err != nil {
		return nil, err
	}
	return types.NormalizeJSON(jsonBytes)
}

// ProtoToEnvelope converts proto to JSON, parses through types.JSONCodec, and attaches the proto value.
func (c *GoogleR4Codec) ProtoToEnvelope(resourceType string, protoVal any) (*types.ResourceEnvelope, error) {
	jsonBytes, err := c.ProtoToJSON(resourceType, protoVal)
	if err != nil {
		return nil, err
	}
	return envelopeFromJSON(c.jsonCodec, resourceType, jsonBytes, protoVal)
}

// wrapR4Resource wraps an individual R4 resource message in a ContainedResource.
func wrapR4Resource(msg proto.Message) (*rpb.ContainedResource, error) {
	rt, err := ResourceTypeOfProto(msg)
	if err != nil {
		return nil, err
	}
	entry, ok := defaultR4Registry.byFHIRType[rt]
	if !ok {
		return nil, errUnsupportedProto
	}

	cr := &rpb.ContainedResource{}
	crMsg := cr.ProtoReflect()
	field := crMsg.Descriptor().Fields().ByName(entry.oneofField)
	if field == nil {
		return nil, fmt.Errorf("proto: missing oneof field %q for %s", entry.oneofField, rt)
	}
	crMsg.Set(field, protoreflect.ValueOfMessage(msg.ProtoReflect()))
	return cr, nil
}
