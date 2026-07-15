package fixtures

import (
	"fmt"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// EnvelopeFromJSON parses FHIR JSON through types.JSONCodec and returns a normalized envelope.
func EnvelopeFromJSON(t *testing.T, resourceType string, data []byte) *types.ResourceEnvelope {
	t.Helper()
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON(resourceType, data)
	if err != nil {
		t.Fatalf("fixtures.EnvelopeFromJSON: %v", err)
	}
	return env
}

// EnvelopeFromProtoJSON parses FHIR JSON through proto.GoogleR4Codec for proto-backed tests.
func EnvelopeFromProtoJSON(t *testing.T, resourceType string, data []byte) *types.ResourceEnvelope {
	t.Helper()
	codec := proto.NewGoogleR4Codec()
	pb, err := codec.ParseJSONToProto(resourceType, data)
	if err != nil {
		t.Fatalf("fixtures.EnvelopeFromProtoJSON ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope(resourceType, pb)
	if err != nil {
		t.Fatalf("fixtures.EnvelopeFromProtoJSON ProtoToEnvelope: %v", err)
	}
	return env
}

// MustEnvelopeFromJSON is like EnvelopeFromJSON but returns an error instead of failing the test.
func MustEnvelopeFromJSON(resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON(resourceType, data)
	if err != nil {
		return nil, fmt.Errorf("fixtures.MustEnvelopeFromJSON: %w", err)
	}
	return env, nil
}
