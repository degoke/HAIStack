package registry_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

type memJobStore struct {
	rows []store.JobRecord
}

func (s *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	s.rows = append(s.rows, job)
	return nil
}

func (s *memJobStore) ClaimNext(context.Context, string) (*store.JobRecord, error) { return nil, nil }
func (s *memJobStore) Update(context.Context, store.JobRecord) error               { return nil }
func (s *memJobStore) Get(context.Context, string) (*store.JobRecord, error)       { return nil, nil }

func TestInstallDefinitionsFromFSEnqueuesOnePackPreExpandJob(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	term := terminology.NewMemoryStore()
	global := terminology.NewMemoryStore()
	jobsStore := &memJobStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		Terminology:         term,
		GlobalTerminology:   global,
		TerminologyScope:    "tenant-a",
		TerminologyInstalls: &memTerminologyInstallStore{},
		JobStore:            jobsStore,
		PreExpandValueSets:  true,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})

	cs := []byte(`{"resourceType":"CodeSystem","id":"cs1","url":"urn:cs","version":"1","concept":[{"code":"a"}]}`)
	vs1 := []byte(`{"resourceType":"ValueSet","id":"vs1","url":"urn:vs1","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	vs2 := []byte(`{"resourceType":"ValueSet","id":"vs2","url":"urn:vs2","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	provenance := registry.InstallProvenance{PackageName: "test-pack", PackageVersion: "1.0"}
	for _, raw := range [][]byte{cs, vs1, vs2} {
		if err := mgr.InstallDefinition(ctx, raw, provenance); err != nil {
			t.Fatal(err)
		}
	}
	if len(jobsStore.rows) != 0 {
		t.Fatalf("InstallDefinition should not enqueue pre-expand jobs, got %d", len(jobsStore.rows))
	}
	if err := mgr.CompletePackageInstall(ctx, provenance); err != nil {
		t.Fatal(err)
	}
	if len(jobsStore.rows) != 1 {
		t.Fatalf("jobs=%d want 1 pack pre-expand job", len(jobsStore.rows))
	}
	if jobsStore.rows[0].Type != jobs.TypeTerminologyPreExpand {
		t.Fatalf("job type=%s", jobsStore.rows[0].Type)
	}
	var payload jobs.TerminologyPreExpandPayload
	if err := jobs.UnmarshalPayload(jobsStore.rows[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PackName != "test-pack" || payload.PackVersion != "1.0" || payload.ScopeID != terminology.GlobalScopeID {
		t.Fatalf("payload=%+v", payload)
	}
	owner, ok := jobs.OwnerFromPayload(jobsStore.rows[0].Payload)
	if !ok || owner.PrincipalID != jobs.RegistryPrincipalID {
		t.Fatalf("owner=%+v ok=%v", owner, ok)
	}
}

func TestInstallDefinitionsFromFSUsesBatchEnqueue(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	term := terminology.NewMemoryStore()
	global := terminology.NewMemoryStore()
	jobsStore := &memJobStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		Terminology:         term,
		GlobalTerminology:   global,
		TerminologyScope:    "tenant-a",
		TerminologyInstalls: &memTerminologyInstallStore{},
		JobStore:            jobsStore,
		PreExpandValueSets:  true,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})

	fsys := fstest.MapFS{
		"vs1.json": &fstest.MapFile{Data: []byte(`{"resourceType":"ValueSet","id":"vs1","url":"urn:vs1","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)},
		"vs2.json": &fstest.MapFile{Data: []byte(`{"resourceType":"ValueSet","id":"vs2","url":"urn:vs2","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)},
		"cs.json":  &fstest.MapFile{Data: []byte(`{"resourceType":"CodeSystem","id":"cs1","url":"urn:cs","version":"1","concept":[{"code":"a"}]}`)},
	}
	if err := mgr.InstallDefinitionsFromFS(ctx, fsys, ".", registry.InstallProvenance{PackageName: "batch-pack", PackageVersion: "2.0"}); err != nil {
		t.Fatal(err)
	}
	if len(jobsStore.rows) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobsStore.rows))
	}
	var payload jobs.TerminologyPreExpandPayload
	if err := jobs.UnmarshalPayload(jobsStore.rows[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PackName != "batch-pack" {
		t.Fatalf("payload=%+v", payload)
	}
}
