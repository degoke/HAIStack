package aitest_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/aitest"
)

func TestHarnessSeedsPatientAndBuildsExecutor(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:     true,
		AllowPatientRead: true,
	})
	if h.Executor == nil {
		t.Fatal("executor is nil")
	}
	ctx := context.Background()
	res, err := h.Resources.Read(ctx, "Patient", "pat-jane")
	if err != nil || res == nil {
		t.Fatalf("read patient: %v, %v", res, err)
	}
}

func TestHarnessWithSearch(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:       true,
		WithSearch:         true,
		AllowPatientSearch: true,
	})
	if h.Search == nil {
		t.Fatal("search service is nil")
	}
}
