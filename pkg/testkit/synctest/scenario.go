package synctest

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	if clock == nil {
		clock = hasync.DefaultClock
	}
	hub := NewMemHub(tenantID)
	hub.SetClock(clock)
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
	if device == nil {
		return false, fmt.Errorf("synctest.ReferenceResolved: device is nil")
	}
	if source == nil {
		return false, fmt.Errorf("synctest.ReferenceResolved: source is nil")
	}
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
	path = strings.NewReplacer("[", ".", "]", "").Replace(path)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("reference path is required")
	}
	parts := strings.Split(path, ".")
	cur := any(obj)
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("invalid reference path %q", path)
		}
		switch node := cur.(type) {
		case map[string]any:
			value, ok := node[part]
			if !ok {
				return "", fmt.Errorf("field %q is missing in path %s", part, path)
			}
			cur = value
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", fmt.Errorf("array index %q is out of range in path %s", part, path)
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
