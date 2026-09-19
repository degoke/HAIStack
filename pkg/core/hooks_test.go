package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/hooks"
)

func TestPreStorageHookCanRejectAndMutate(t *testing.T) {
	ctx := context.Background()
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
		if event.Action != hooks.ActionCreate {
			t.Fatalf("action = %q", event.Action)
		}
		if strings.Contains(string(event.Resource.JSON), "Blocked") {
			return errors.New("blocked family")
		}
		event.Resource.JSON = []byte(`{"resourceType":"Patient","id":"pat-1","name":[{"family":"Hooked"}]}`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	mem := newMemBackend()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		IDPolicy:  core.DefaultIDPolicy{},
		Hooks:     reg,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Create(ctx, patientEnvelope("pat-block", "Blocked")); err == nil {
		t.Fatal("expected pre-storage rejection")
	}

	created, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(string(created.JSON), "Hooked") {
		t.Fatalf("expected mutated JSON, got %s", created.JSON)
	}
}

func TestPostCommitHookSeesPersistedResource(t *testing.T) {
	ctx := context.Background()
	reg := hooks.NewRegistry()
	var seen string
	if err := reg.On(hooks.PostCommit, func(_ context.Context, event *hooks.Event) error {
		seen = event.ID
		return errors.New("post-commit must not fail the write")
	}); err != nil {
		t.Fatal(err)
	}

	mem := newMemBackend()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		IDPolicy:  core.DefaultIDPolicy{},
		Hooks:     reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.Create(ctx, patientEnvelope("pat-2", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "pat-2" {
		t.Fatalf("id = %q", created.ID)
	}
	if seen != "pat-2" {
		t.Fatalf("post-commit id = %q", seen)
	}
	if _, err := svc.Read(ctx, "Patient", "pat-2"); err != nil {
		t.Fatalf("Read after post-commit error: %v", err)
	}
}

func TestPreStorageHookServiceErrorPreserved(t *testing.T) {
	ctx := context.Background()
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(context.Context, *hooks.Event) error {
		return &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "writes disabled"}
	}); err != nil {
		t.Fatal(err)
	}
	mem := newMemBackend()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		Hooks:     reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(ctx, patientEnvelope("pat-3", "Doe"))
	if core.KindOf(err) != core.ErrorKindNotSupported {
		t.Fatalf("expected not-supported, got %v kind %q", err, core.KindOf(err))
	}
}

func TestNoHooksIsNoop(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	if _, err := harness.svc.Create(context.Background(), patientEnvelope("pat-ok", "Doe")); err != nil {
		t.Fatal(err)
	}
}
