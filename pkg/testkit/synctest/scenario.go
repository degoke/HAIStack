package synctest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Scenario wires two device nodes to a shared fake hub.
type Scenario struct {
	Hub      *MemHub
	DeviceA  *Device
	DeviceB  *Device
	TenantID string
	Clock    hasync.Clock
}

// NewScenario creates a two-device sync test scenario with a fresh hub.
func NewScenario(tenantID string, clock hasync.Clock) *Scenario {
	hub := NewMemHub()
	if clock == nil {
		clock = hasync.DefaultClock
	}
	return &Scenario{
		Hub:      hub,
		DeviceA:  NewDevice("device-a", tenantID, hub, clock),
		DeviceB:  NewDevice("device-b", tenantID, hub, clock),
		TenantID: tenantID,
		Clock:    clock,
	}
}

// ScenarioResult captures artifacts from a sync scenario run.
type ScenarioResult struct {
	PushSummary      *hasync.PushResultSummary
	PullSummary      *hasync.PullResultSummary
	HubEvents        []hasync.CanonicalEvent
	DeviceBResources []*types.ResourceEnvelope
	Conflicts        []store.ConflictRecord
	AuditRecords     []store.AuditRecord
}

// RunPushPull seeds nothing; it pushes from device A then pulls on device B.
func (s *Scenario) RunPushPull(ctx context.Context) (*ScenarioResult, error) {
	push, err := s.DeviceA.Push(ctx)
	if err != nil {
		return nil, fmt.Errorf("device A push: %w", err)
	}
	pull, err := s.DeviceB.Pull(ctx)
	if err != nil {
		return nil, fmt.Errorf("device B pull: %w", err)
	}
	return s.buildResult(push, pull), nil
}

func (s *Scenario) buildResult(push *hasync.PushResultSummary, pull *hasync.PullResultSummary) *ScenarioResult {
	return &ScenarioResult{
		PushSummary:      push,
		PullSummary:      pull,
		HubEvents:        s.Hub.CanonicalEvents(),
		DeviceBResources: s.DeviceB.Backend.Resources.All(),
		Conflicts:        s.DeviceA.Backend.Conflicts.Records(),
		AuditRecords:     append(s.DeviceA.Backend.Audit.Records(), s.DeviceB.Backend.Audit.Records()...),
	}
}

// ReferenceResolved reports whether targetType/targetID exists on device B when
// source references it via a FHIR reference string in JSON.
func ReferenceResolved(ctx context.Context, device *Device, source *types.ResourceEnvelope, refPath string, targetType, targetID string) (bool, error) {
	ref, err := extractReference(source.JSON, refPath)
	if err != nil {
		return false, err
	}
	expected := targetType + "/" + targetID
	if ref != expected {
		return false, fmt.Errorf("reference at %s = %q, want %q", refPath, ref, expected)
	}
	return device.ResourceExists(ctx, targetType, targetID)
}

func extractReference(jsonData []byte, path string) (string, error) {
	var obj map[string]any
	if err := json.Unmarshal(jsonData, &obj); err != nil {
		return "", err
	}
	parts := strings.Split(path, ".")
	cur := any(obj)
	for _, part := range parts {
		switch node := cur.(type) {
		case map[string]any:
			cur = node[part]
		case []any:
			idx := 0
			if part != "0" {
				return "", fmt.Errorf("unsupported array index %q in path %s", part, path)
			}
			if len(node) == 0 {
				return "", fmt.Errorf("empty array at %s", path)
			}
			cur = node[idx]
		default:
			return "", fmt.Errorf("cannot traverse %q in path %s", part, path)
		}
	}
	refObj, ok := cur.(map[string]any)
	if !ok {
		return "", fmt.Errorf("reference path %s is not an object", path)
	}
	ref, _ := refObj["reference"].(string)
	return ref, nil
}

// OfflineCreateAndSync is a reusable flow: seed on device A, push, pull on device B.
func OfflineCreateAndSync(ctx context.Context, s *Scenario, resources ...*types.ResourceEnvelope) (*ScenarioResult, error) {
	ts := s.Clock()
	for _, res := range resources {
		if err := s.DeviceA.SeedLocalCreate(ctx, res, ts); err != nil {
			return nil, fmt.Errorf("seed %s/%s: %w", res.ResourceType, res.ID, err)
		}
	}
	return s.RunPushPull(ctx)
}

// CanonicalSequenceGrowth returns the highest canonical sequence in the hub log.
func CanonicalSequenceGrowth(hub *MemHub) int64 {
	events := hub.CanonicalEvents()
	var max int64
	for _, e := range events {
		if e.CanonicalSequence > max {
			max = e.CanonicalSequence
		}
	}
	return max
}

// At is a test helper for fixed scenario clocks.
func At(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}
