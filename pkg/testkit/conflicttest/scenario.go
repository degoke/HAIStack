package conflicttest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/testkit/synctest"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

// Scenario runs conflict detection and optional resolution through sync + conflict.
type Scenario struct {
	Hub      *synctest.MemHub
	DeviceA  *synctest.Device
	DeviceB  *synctest.Device
	Engine   *conflict.Engine
	Clock    hasync.Clock
	TenantID string
}

// NewScenario creates a conflict test scenario with stale-base detection enabled.
func NewScenario(tenantID string, clock hasync.Clock) *Scenario {
	hub := synctest.NewMemHub()
	hub.SetStaleOnMismatch(true)
	if clock == nil {
		clock = hasync.DefaultClock
	}
	engine := conflict.NewDefaultEngine()
	deviceA := synctest.NewDevice("node-a", tenantID, hub, clock).WithConflictEngine(engine)
	deviceB := synctest.NewDevice("node-b", tenantID, hub, clock)
	return &Scenario{
		Hub:      hub,
		DeviceA:  deviceA,
		DeviceB:  deviceB,
		Engine:   engine,
		Clock:    clock,
		TenantID: tenantID,
	}
}

// EditNodes holds the two concurrent edit envelopes for a conflict scenario.
type EditNodes struct {
	Base   *types.ResourceEnvelope
	LocalA *types.ResourceEnvelope
	LocalB *types.ResourceEnvelope
	Cloud  *types.ResourceEnvelope
}

// Result captures conflict evaluation and sync artifacts.
type Result struct {
	DetectResult    conflict.Result
	MergeResult     conflict.MergeResult
	PushSummary     *hasync.PushResultSummary
	Conflicts       []store.ConflictRecord
	CanonicalEvents []hasync.CanonicalEvent
	ResolutionPush  []hasync.PushResult
	CanonicalCount  int
	AuditRecords    []store.AuditRecord
	AutoMerged      bool
	NeedsReview     bool
}

// LocalUpdate builds a conflict.LocalEvent for evaluation.
func LocalUpdate(resourceType, id, baseVersion, localVersion string, after *types.ResourceEnvelope) conflict.LocalEvent {
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

// EnvelopeFromFields builds a normalized envelope from a field map.
func EnvelopeFromFields(resourceType, id, version string, fields map[string]any) (*types.ResourceEnvelope, error) {
	obj := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	for k, v := range fields {
		obj[k] = v
	}
	if version != "" {
		obj["meta"] = map[string]any{"versionId": version}
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return types.NewJSONCodec().ParseJSON(resourceType, data)
}

// Evaluate runs conflict.Detect and conflict.Merge for a local edit triple.
func (s *Scenario) Evaluate(local conflict.LocalEvent, base, current *types.ResourceEnvelope) Result {
	detect := s.Engine.Detect(local, base, current)
	merge := s.Engine.Merge(local, base, current)
	return Result{
		DetectResult: detect,
		MergeResult:  merge,
		AutoMerged:   merge.AutoMergeable,
		NeedsReview:  !merge.AutoMergeable && detect.Classification != conflict.ClassificationNoConflict,
	}
}

// RunStaleBaseConflict seeds node A's edit against a newer hub version and pushes.
func (s *Scenario) RunStaleBaseConflict(ctx context.Context, edits EditNodes) (*Result, error) {
	if edits.Base == nil || edits.LocalA == nil || edits.Cloud == nil {
		return nil, fmt.Errorf("conflicttest.RunStaleBaseConflict: base, localA, and cloud are required")
	}

	s.Hub.SeedResource(edits.Cloud)
	ts := s.Clock()

	if err := s.DeviceA.Backend.History.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: edits.Base.ResourceType,
		ID:           edits.Base.ID,
		VersionID:    edits.Base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    ts,
		Resource:     edits.Base,
	}); err != nil {
		return nil, err
	}
	if err := s.DeviceA.Backend.History.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: edits.LocalA.ResourceType,
		ID:           edits.LocalA.ID,
		VersionID:    edits.LocalA.VersionID,
		Action:       store.VersionActionUpdate,
		Timestamp:    ts,
		Resource:     edits.LocalA,
	}); err != nil {
		return nil, err
	}
	if _, err := s.DeviceA.Backend.Events.Append(ctx, store.ResourceEvent{
		ResourceType: edits.LocalA.ResourceType,
		ID:           edits.LocalA.ID,
		VersionID:    edits.LocalA.VersionID,
		Action:       store.EventActionUpdate,
		Timestamp:    ts,
		Hash:         edits.LocalA.Hash,
	}); err != nil {
		return nil, err
	}

	eval := s.Evaluate(
		LocalUpdate(edits.LocalA.ResourceType, edits.LocalA.ID, edits.Base.VersionID, edits.LocalA.VersionID, edits.LocalA),
		edits.Base,
		edits.Cloud,
	)

	push, err := s.DeviceA.Push(ctx)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	eval.PushSummary = push
	eval.Conflicts = s.DeviceA.Backend.Conflicts.Records()
	eval.CanonicalEvents = s.Hub.CanonicalEvents()
	eval.CanonicalCount = len(eval.CanonicalEvents)
	eval.AuditRecords = s.DeviceA.Backend.Audit.Records()
	return &eval, nil
}

// RunTwoNodeStaleBaseConflict simulates the full two-device flow:
// node B edits and pushes first, then node A pushes a stale-base edit and conflicts.
func (s *Scenario) RunTwoNodeStaleBaseConflict(ctx context.Context, edits EditNodes) (*Result, error) {
	if edits.Base == nil || edits.LocalA == nil || edits.LocalB == nil {
		return nil, fmt.Errorf("conflicttest.RunTwoNodeStaleBaseConflict: base, localA, and localB are required")
	}

	ts := s.Clock()
	s.Hub.SeedResource(edits.Base)
	for _, device := range []*synctest.Device{s.DeviceA, s.DeviceB} {
		if err := device.Backend.Resources.Create(ctx, edits.Base); err != nil {
			return nil, fmt.Errorf("seed base current state on %s: %w", device.NodeID, err)
		}
		if err := device.Backend.History.AppendVersion(ctx, store.ResourceVersion{
			ResourceType: edits.Base.ResourceType,
			ID:           edits.Base.ID,
			VersionID:    edits.Base.VersionID,
			Action:       store.VersionActionCreate,
			Timestamp:    ts,
			Resource:     edits.Base,
			Hash:         edits.Base.Hash,
		}); err != nil {
			return nil, fmt.Errorf("seed base history on %s: %w", device.NodeID, err)
		}
	}

	if err := s.DeviceB.SeedLocalUpdate(ctx, edits.LocalB, ts); err != nil {
		return nil, fmt.Errorf("seed node B update: %w", err)
	}
	if _, err := s.DeviceB.Push(ctx); err != nil {
		return nil, fmt.Errorf("node B push: %w", err)
	}

	currentMap := s.Hub.Resources()
	current, ok := currentMap[testkit.ResourceKey(edits.LocalB.ResourceType, edits.LocalB.ID)]
	if !ok {
		return nil, fmt.Errorf("hub current resource missing after node B push")
	}

	if err := s.DeviceA.SeedLocalUpdate(ctx, edits.LocalA, ts); err != nil {
		return nil, fmt.Errorf("seed node A update: %w", err)
	}

	eval := s.Evaluate(
		LocalUpdate(edits.LocalA.ResourceType, edits.LocalA.ID, edits.Base.VersionID, edits.LocalA.VersionID, edits.LocalA),
		edits.Base,
		current,
	)

	push, err := s.DeviceA.Push(ctx)
	if err != nil {
		return nil, fmt.Errorf("node A push: %w", err)
	}
	eval.PushSummary = push
	eval.Conflicts = s.DeviceA.Backend.Conflicts.Records()
	eval.CanonicalEvents = s.Hub.CanonicalEvents()
	eval.CanonicalCount = len(eval.CanonicalEvents)
	eval.AuditRecords = s.DeviceA.Backend.Audit.Records()
	return &eval, nil
}

// RunAutoMergeResolution processes a conflict job when the engine auto-merges.
func (s *Scenario) RunAutoMergeResolution(ctx context.Context, edits EditNodes) (*Result, error) {
	if edits.Base == nil || edits.LocalA == nil || edits.Cloud == nil {
		return nil, fmt.Errorf("conflicttest.RunAutoMergeResolution: base, localA, and cloud are required")
	}

	now := s.Clock()
	s.Hub.SeedResource(edits.Cloud)
	if err := s.DeviceA.Backend.History.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: edits.Base.ResourceType,
		ID:           edits.Base.ID,
		VersionID:    edits.Base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     edits.Base,
		Hash:         edits.Base.Hash,
	}); err != nil {
		return nil, err
	}
	if err := s.DeviceA.Backend.Resources.Create(ctx, edits.Cloud); err != nil {
		return nil, err
	}
	if err := s.DeviceA.Backend.Conflicts.Append(ctx, store.ConflictRecord{
		ID:           "conflict-1",
		ResourceType: edits.LocalA.ResourceType,
		ResourceID:   edits.LocalA.ID,
		CreatedAt:    now,
	}); err != nil {
		return nil, err
	}

	localEvent := hasync.LocalEvent{
		EventID:          "event-1",
		OriginNodeID:     s.DeviceA.NodeID,
		TenantID:         s.TenantID,
		ResourceType:     edits.LocalA.ResourceType,
		ResourceID:       edits.LocalA.ID,
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: edits.Base.VersionID,
		LocalVersion:     edits.LocalA.VersionID,
		ResourceAfter:    edits.LocalA,
	}
	localEventJSON, _ := json.Marshal(localEvent)
	payload, _ := json.Marshal(hasync.ConflictJobPayload{
		NodeID:          s.DeviceA.NodeID,
		TenantID:        s.TenantID,
		ConflictID:      "conflict-1",
		EventID:         localEvent.EventID,
		ResourceType:    edits.LocalA.ResourceType,
		ResourceID:      edits.LocalA.ID,
		LocalVersionID:  edits.LocalA.VersionID,
		RemoteVersionID: edits.Cloud.VersionID,
		Reason:          "stale base version",
		LocalEvent:      localEventJSON,
	})
	_ = s.DeviceA.Backend.Jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      hasync.JobTypeConflictProcessing,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})

	handler := &autoMergePushHandler{
		Hub:      s.Hub,
		NodeID:   s.DeviceA.NodeID,
		TenantID: s.TenantID,
	}

	processor := &hasync.JobProcessor{
		Engine: hasync.NewEngine(hasync.Config{
			NodeID:                    s.DeviceA.NodeID,
			TenantID:                  s.TenantID,
			Resources:                 s.DeviceA.Backend.Resources,
			History:                   s.DeviceA.Backend.History,
			Conflicts:                 s.DeviceA.Backend.Conflicts,
			Jobs:                      s.DeviceA.Backend.Jobs,
			Audit:                     s.DeviceA.Backend.Audit,
			ConflictEngine:            s.Engine,
			ConflictResolutionHandler: handler,
			Clock:                     s.Clock,
		}),
		Jobs:  s.DeviceA.Backend.Jobs,
		Clock: s.Clock,
	}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		return nil, fmt.Errorf("ProcessNext: %w", err)
	}
	if !processed {
		return nil, fmt.Errorf("expected conflict job to be processed")
	}

	eval := s.Evaluate(
		LocalUpdate(edits.LocalA.ResourceType, edits.LocalA.ID, edits.Base.VersionID, edits.LocalA.VersionID, edits.LocalA),
		edits.Base,
		edits.Cloud,
	)
	eval.CanonicalEvents = s.Hub.CanonicalEvents()
	eval.CanonicalCount = len(eval.CanonicalEvents)
	eval.ResolutionPush = handler.Results()
	eval.Conflicts = s.DeviceA.Backend.Conflicts.Records()
	eval.AuditRecords = s.DeviceA.Backend.Audit.Records()
	eval.AutoMerged = hasAuditAction(eval.AuditRecords, hasync.AuditConflictAutoMerged)
	return &eval, nil
}

type autoMergePushHandler struct {
	Hub      hasync.Hub
	NodeID   string
	TenantID string
	results  []hasync.PushResult
}

func (h *autoMergePushHandler) OnConflictResolution(ctx context.Context, payload hasync.ConflictJobPayload, result conflict.MergeResult) error {
	if h == nil || h.Hub == nil || !result.AutoMergeable || result.Merged == nil {
		return nil
	}
	baseVersion := payload.RemoteVersionID
	if baseVersion == "" {
		baseVersion = result.Merged.VersionID
	}
	pushResults, err := h.Hub.Push(ctx, []hasync.LocalEvent{{
		EventID:          payload.EventID + ".merged",
		OriginNodeID:     h.NodeID,
		TenantID:         h.TenantID,
		ResourceType:     result.Merged.ResourceType,
		ResourceID:       result.Merged.ID,
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: baseVersion,
		LocalVersion:     payload.LocalVersionID,
		ResourceAfter:    result.Merged,
		ResourceHash:     result.Merged.Hash,
	}})
	if err != nil {
		return err
	}
	h.results = append(h.results, pushResults...)
	return nil
}

func (h *autoMergePushHandler) Results() []hasync.PushResult {
	out := make([]hasync.PushResult, len(h.results))
	copy(out, h.results)
	return out
}

func hasAuditAction(records []store.AuditRecord, action string) bool {
	for _, rec := range records {
		if rec.Action == action {
			return true
		}
	}
	return false
}

// DefaultConcurrentPatientEdits returns a standard non-overlapping edit triple.
func DefaultConcurrentPatientEdits() (EditNodes, error) {
	base, err := EnvelopeFromFields("Patient", "p1", "base-v1", map[string]any{})
	if err != nil {
		return EditNodes{}, err
	}
	localA, err := EnvelopeFromFields("Patient", "p1", "local-v2", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	if err != nil {
		return EditNodes{}, err
	}
	localB, err := EnvelopeFromFields("Patient", "p1", "local-b-v2", map[string]any{
		"address": []any{map[string]any{"city": "NYC"}},
	})
	if err != nil {
		return EditNodes{}, err
	}
	cloud, err := EnvelopeFromFields("Patient", "p1", "cloud-v2", map[string]any{
		"address": []any{map[string]any{"city": "NYC"}},
	})
	if err != nil {
		return EditNodes{}, err
	}
	return EditNodes{Base: base, LocalA: localA, LocalB: localB, Cloud: cloud}, nil
}

// StoreBackend exposes storetest backend helpers for assertions.
func StoreBackend(d *synctest.Device) *storetest.Backend {
	return d.Backend
}
