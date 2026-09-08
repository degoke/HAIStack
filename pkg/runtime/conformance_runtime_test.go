package runtime_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestRuntimeExposesConformanceRuntime(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/conformance.db").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()
	if rt.Services().ConformanceRuntime == nil {
		t.Fatal("expected ConformanceRuntime to be wired")
	}
	if rt.Services().ConformanceRuntime.Snapshot() == nil {
		t.Fatal("expected snapshot on conformance runtime")
	}
}
