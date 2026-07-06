package conflict_test

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func resourceEnvelope(t *testing.T, resourceType, id, version string, fields map[string]any) *types.ResourceEnvelope {
	t.Helper()
	obj := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	for k, v := range fields {
		obj[k] = v
	}
	return &types.ResourceEnvelope{
		ResourceType: resourceType,
		ID:           id,
		VersionID:    version,
		JSON:         mustJSON(t, obj),
	}
}

func localUpdate(resourceType, id, baseVersion, localVersion string, after *types.ResourceEnvelope) conflict.LocalEvent {
	return conflict.LocalEvent{
		EventID:          resourceType + "/" + id + "/" + localVersion,
		ResourceType:     resourceType,
		ResourceID:       id,
		Operation:        "resource.updated",
		BaseCloudVersion: baseVersion,
		LocalVersion:     localVersion,
		ResourceAfter:    after,
	}
}
