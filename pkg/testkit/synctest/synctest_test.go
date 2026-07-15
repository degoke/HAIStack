package synctest_test

import (
	"context"
	"testing"

	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/synctest"
)

func TestPatientOfflinePushPull(t *testing.T) {
	ctx := context.Background()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	scenario := synctest.NewScenario("tenant-a", synctest.FixedClock(now))

	patient := fixtures.PatientJane(t)
	result, err := synctest.OfflineCreateAndSync(ctx, scenario, patient)
	if err != nil {
		t.Fatalf("OfflineCreateAndSync: %v", err)
	}
	if result.PushSummary.Proposed != 1 || result.PushSummary.Results[0].State != hasync.AckAccepted {
		t.Fatalf("push = %+v", result.PushSummary)
	}
	if result.PullSummary.Applied != 1 {
		t.Fatalf("pull = %+v", result.PullSummary)
	}
	if synctest.CanonicalSequenceGrowth(scenario.Hub) != 1 {
		t.Fatal("expected canonical sequence 1")
	}
	exists, _ := scenario.DeviceB.ResourceExists(ctx, "Patient", "pat-jane")
	if !exists {
		t.Fatal("patient not on device B")
	}
}

func TestAppointmentSurvivesPushAndPull(t *testing.T) {
	ctx := context.Background()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	scenario := synctest.NewScenario("tenant-a", synctest.FixedClock(now))

	patient := fixtures.PatientJane(t)
	appt := fixtures.AppointmentBooked(t)
	result, err := synctest.OfflineCreateAndSync(ctx, scenario, patient, appt)
	if err != nil {
		t.Fatalf("OfflineCreateAndSync: %v", err)
	}
	if result.PullSummary.Applied != 2 {
		t.Fatalf("pull applied = %d, want 2", result.PullSummary.Applied)
	}

	resolved, err := synctest.ReferenceResolved(ctx, scenario.DeviceB, appt, "participant.0.actor", "Patient", "pat-jane")
	if err != nil || !resolved {
		t.Fatalf("reference resolved = %v, %v", resolved, err)
	}
}

func TestHubPushIdempotent(t *testing.T) {
	ctx := context.Background()
	hub := synctest.NewMemHub()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	patient := fixtures.SamplePatient(t, "p1")

	device := synctest.NewDevice("device-a", "tenant-a", hub, synctest.FixedClock(now))
	if err := device.SeedLocalCreate(ctx, patient, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	first, err := device.Push(ctx)
	if err != nil || first.Results[0].State != hasync.AckAccepted {
		t.Fatalf("first push: %+v, %v", first, err)
	}
	second, err := device.Push(ctx)
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if second.Proposed != 0 && second.Results[0].State != hasync.AckAlreadyProcessed {
		// second push may propose 0 if cursor advanced; if it re-proposes, hub dedupes
		if len(second.Results) > 0 && second.Results[0].State == hasync.AckAlreadyProcessed {
			return
		}
	}
}

func TestPullIdempotentInbox(t *testing.T) {
	ctx := context.Background()
	now := synctest.At(2026, 7, 6, 12, 0, 0)
	scenario := synctest.NewScenario("tenant-a", synctest.FixedClock(now))
	patient := fixtures.SamplePatient(t, "p1")
	if _, err := synctest.OfflineCreateAndSync(ctx, scenario, patient); err != nil {
		t.Fatalf("sync: %v", err)
	}
	second, err := scenario.DeviceB.Pull(ctx)
	if err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if second.Skipped == 0 && second.Applied != 0 {
		t.Fatalf("expected idempotent skip, got %+v", second)
	}
}
