package jobs

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type memJobStore struct {
	job store.JobRecord
}

func (m *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	m.job = job
	return nil
}
func (m *memJobStore) ClaimNext(context.Context, string) (*store.JobRecord, error) { return nil, nil }
func (m *memJobStore) Update(_ context.Context, job store.JobRecord) error {
	m.job = job
	return nil
}
func (m *memJobStore) Get(context.Context, string) (*store.JobRecord, error) { return &m.job, nil }

func TestReporterUpdateAndComplete(t *testing.T) {
	ctx := context.Background()
	st := &memJobStore{}
	job, err := NewJob(TypeRegistryPackageInstall, PackageInstallPayload{Source: "registry", PackageID: "pkg", Version: "1.0"}, EnqueueOptions{ID: "job-1"})
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewReporter(st, job)
	if err := reporter.Update(ctx, Progress{Phase: "install", Current: 2, Total: 5, Message: "def"}); err != nil {
		t.Fatal(err)
	}
	progress, err := ProgressFromPayload(st.job.Payload)
	if err != nil || progress == nil || progress.Current != 2 || progress.Total != 5 {
		t.Fatalf("progress=%+v err=%v", progress, err)
	}
	if err := reporter.Complete(ctx, map[string]any{"installed": 5}); err != nil {
		t.Fatal(err)
	}
	result, err := ResultFromPayload(st.job.Payload)
	if err != nil || result == nil || result["installed"] != float64(5) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
