package proto

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/proto/r4"
	"github.com/degoke/health-ai-stack/pkg/types"
	rpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/bundle_and_contained_resource_go_proto"
)

// AsContainedResource returns v as a Google R4 ContainedResource.
// Values already stored as ContainedResource are returned as-is. Individual R4
// resource messages (for example *r4.Patient) are wrapped automatically.
func AsContainedResource(v any) (*r4.ContainedResource, error) {
	if isNilValue(v) {
		return nil, errNilProto
	}
	msg, err := asProtoMessage(v)
	if err != nil {
		return nil, err
	}

	if cr, ok := msg.(*rpb.ContainedResource); ok {
		return cr, nil
	}

	wrapped, err := wrapR4Resource(msg)
	if err != nil {
		return nil, err
	}
	return wrapped, nil
}

// ContainedResourceFromEnvelope returns envelope.Proto as a ContainedResource.
// It errors when envelope is nil, Proto is unset, or Proto is not a supported R4 value.
func ContainedResourceFromEnvelope(envelope *types.ResourceEnvelope) (*r4.ContainedResource, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope is nil")
	}
	if envelope.Proto == nil {
		return nil, fmt.Errorf("envelope has no proto value")
	}
	return AsContainedResource(envelope.Proto)
}
