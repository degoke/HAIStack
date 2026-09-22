package validate

import (
	"context"
	"fmt"

	"github.com/degoke/haistack/pkg/proto"
	"github.com/degoke/haistack/pkg/types"
)

func (e *builtinEngine) validateStructure(ctx context.Context, resourceType string, res *types.ResourceEnvelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if canReuseEnvelopeProto(resourceType, res) {
		return nil
	}
	if _, err := e.protoCodec.ParseJSONToProto(resourceType, res.JSON); err != nil {
		return err
	}
	return validateAttachedProtoType(resourceType, res)
}

// canReuseEnvelopeProto reports whether envelope.Proto can substitute for re-parsing JSON.
// Reuse is safe only when proto is supported, matches the JSON resource type, and the
// envelope hash still matches canonical JSON (proto was attached from the same payload).
func canReuseEnvelopeProto(resourceType string, res *types.ResourceEnvelope) bool {
	if res == nil || res.Proto == nil || !proto.IsProtoResource(res.Proto) {
		return false
	}
	rt, err := proto.ResourceTypeOfProto(res.Proto)
	if err != nil || rt != resourceType {
		return false
	}
	if res.Hash == "" {
		return false
	}
	hash, err := types.HashResource(res.JSON)
	if err != nil || hash != res.Hash {
		return false
	}
	return true
}

func validateAttachedProtoType(resourceType string, res *types.ResourceEnvelope) error {
	if res == nil || res.Proto == nil || !proto.IsProtoResource(res.Proto) {
		return nil
	}
	rt, err := proto.ResourceTypeOfProto(res.Proto)
	if err != nil {
		return err
	}
	if rt != resourceType {
		return fmt.Errorf("resourceType mismatch between JSON (%s) and envelope proto (%s)", resourceType, rt)
	}
	return nil
}
