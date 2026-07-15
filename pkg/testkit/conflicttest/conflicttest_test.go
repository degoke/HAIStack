package conflicttest_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit/conflicttest"
	"github.com/degoke/health-ai-stack/pkg/testkit/synctest"
)

func TestStaleBaseConflictDetection(t *testing.T) {
	ctx := context.Background()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	scenario := conflicttest.NewScenario("tenant-a", synctest.FixedClock(now))

	edits, err := conflicttest.DefaultConcurrentPatientEdits()
	if err != nil {
		t.Fatalf("DefaultConcurrentPatientEdits: %v", err)
	}

	result, err := scenario.RunTwoNodeStaleBaseConflict(ctx, edits)
	if err != nil {
		t.Fatalf("RunTwoNodeStaleBaseConflict: %v", err)
	}
	if result.PushSummary.Results[0].State != hasync.AckConflicted {
		t.Fatalf("state = %q", result.PushSummary.Results[0].State)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("conflicts = %d", len(result.Conflicts))
	}
	if result.CanonicalCount != 1 {
		t.Fatalf("canonical count = %d, want 1", result.CanonicalCount)
	}
	if result.DetectResult.AutoMergeable != result.MergeResult.AutoMergeable {
		t.Fatal("detect/merge auto-mergeable mismatch")
	}
}

func TestConflictEvaluationMetadata(t *testing.T) {
	scenario := conflicttest.NewScenario("tenant-a", synctest.FixedClock(synctest.At(2026, 1, 1, 0, 0, 0)))
	edits, err := conflicttest.DefaultConcurrentPatientEdits()
	if err != nil {
		t.Fatalf("edits: %v", err)
	}
	local := conflicttest.LocalUpdate("Patient", "p1", edits.Base.VersionID, edits.LocalA.VersionID, edits.LocalA)
	result := scenario.Evaluate(local, edits.Base, edits.Cloud)
	if result.DetectResult.Classification == conflict.ClassificationNoConflict {
		t.Fatalf("expected conflict, got %q", result.DetectResult.Classification)
	}
	if !result.AutoMerged {
		t.Fatal("expected auto-mergeable non-overlapping patient edits")
	}
}

func TestAutoMergeResolutionAudit(t *testing.T) {
	ctx := context.Background()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	scenario := conflicttest.NewScenario("tenant-a", synctest.FixedClock(now))
	edits, err := conflicttest.DefaultConcurrentPatientEdits()
	if err != nil {
		t.Fatalf("edits: %v", err)
	}
	result, err := scenario.RunAutoMergeResolution(ctx, edits)
	if err != nil {
		t.Fatalf("RunAutoMergeResolution: %v", err)
	}
	if !result.AutoMerged {
		t.Fatalf("expected auto-merge audit, records=%+v", result.AuditRecords)
	}
	if len(result.ResolutionPush) != 1 || result.ResolutionPush[0].State != hasync.AckAccepted {
		t.Fatalf("resolution push = %+v", result.ResolutionPush)
	}
	if result.CanonicalCount != 1 {
		t.Fatalf("canonical count = %d, want 1", result.CanonicalCount)
	}
	if len(result.CanonicalEvents) != 1 || result.CanonicalEvents[0].Status != hasync.CanonicalStatusAccepted {
		t.Fatalf("canonical events = %+v", result.CanonicalEvents)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].ResolvedAt == nil {
		t.Fatalf("conflicts = %+v", result.Conflicts)
	}
}
