package proto

import "github.com/degoke/health-ai-stack/pkg/types"

// ProtoCodec converts between FHIR JSON, provider-specific protobuf messages,
// and types.ResourceEnvelope values.
type ProtoCodec interface {
	ParseJSONToProto(resourceType string, data []byte) (any, error)
	ParseJSONToEnvelope(resourceType string, data []byte) (*types.ResourceEnvelope, error)
	ProtoToJSON(resourceType string, proto any) ([]byte, error)
	ProtoToEnvelope(resourceType string, proto any) (*types.ResourceEnvelope, error)
}
