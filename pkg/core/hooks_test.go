package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/hooks"
	"github.com/degoke/health-ai-stack/pkg/types"
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

func TestPatchHooksUseActionPatch(t *testing.T) {
	ctx := context.Background()
	var pre, post hooks.Action
	var prePrevious, postPrevious bool
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
		if event.Action == hooks.ActionCreate {
			return nil
		}
		pre = event.Action
		prePrevious = event.Previous != nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.On(hooks.PostCommit, func(_ context.Context, event *hooks.Event) error {
		if event.Action == hooks.ActionCreate {
			return nil
		}
		post = event.Action
		postPrevious = event.Previous != nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := newHookedService(t, reg)
	if _, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	patch := []byte(`[{"op":"replace","path":"/name/0/family","value":"Smith"}]`)
	if _, err := svc.Patch(ctx, "Patient", "pat-1", patch); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if pre != hooks.ActionPatch {
		t.Fatalf("pre-storage action = %q, want patch", pre)
	}
	if post != hooks.ActionPatch {
		t.Fatalf("post-commit action = %q, want patch", post)
	}
	if !prePrevious {
		t.Fatal("expected non-nil Previous on patch pre-storage")
	}
	if !postPrevious {
		t.Fatal("expected non-nil Previous on patch post-commit")
	}
}

func TestUpdatePreStorageReceivesPrevious(t *testing.T) {
	ctx := context.Background()
	var sawPrevious bool
	var previousFamily string
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
		if event.Action != hooks.ActionUpdate {
			return nil
		}
		if event.Previous == nil {
			t.Fatal("Previous was nil on update pre-storage")
		}
		sawPrevious = true
		if event.Previous.ID != "pat-1" {
			t.Fatalf("Previous.ID = %q", event.Previous.ID)
		}
		previousFamily = string(event.Previous.JSON)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := newHookedService(t, reg)
	if _, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(ctx, patientEnvelope("pat-1", "Smith")); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !sawPrevious {
		t.Fatal("update pre-storage did not run")
	}
	if !strings.Contains(previousFamily, "Doe") {
		t.Fatalf("Previous JSON = %s, want original family Doe", previousFamily)
	}
}

func TestPreStorageIdentityMutationRejected(t *testing.T) {
	ctx := context.Background()
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
		if event.Action != hooks.ActionUpdate {
			return nil
		}
		event.Resource.ID = "hijacked"
		event.Resource.JSON = []byte(`{"resourceType":"Patient","id":"hijacked","name":[{"family":"X"}]}`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := newHookedService(t, reg)
	if _, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := svc.Update(ctx, patientEnvelope("pat-1", "Smith"))
	if err == nil || core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("expected invalid identity mutation, got %v kind %q", err, core.KindOf(err))
	}
	if _, err := svc.Read(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("original resource missing after rejected mutation: %v", err)
	}
	if _, err := svc.Read(ctx, "Patient", "hijacked"); !core.IsNotFound(err) {
		t.Fatalf("hijacked id should not exist, got %v", err)
	}
}

func TestTransactionPostCommitSeesBundleJSON(t *testing.T) {
	ctx := context.Background()
	var postJSON string
	var postType string
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PostCommit, func(_ context.Context, event *hooks.Event) error {
		if event.Action != hooks.ActionTransaction {
			return nil
		}
		if event.Resource == nil {
			t.Fatal("transaction post-commit resource was nil")
		}
		postType = event.Resource.ResourceType
		postJSON = string(event.Resource.JSON)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := newHookedService(t, reg)
	bundle := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","name":[{"family":"Txn"}]}}]}`)
	resp, err := svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{ResourceType: "Bundle", JSON: bundle})
	if err != nil {
		t.Fatalf("ProcessTransactionBundle: %v", err)
	}
	if postType != "Bundle" {
		t.Fatalf("post-commit resourceType = %q", postType)
	}
	if postJSON == "" {
		t.Fatal("expected transaction post-commit JSON")
	}
	if !strings.Contains(postJSON, "transaction-response") && !strings.Contains(postJSON, `"entry"`) {
		t.Fatalf("post-commit JSON missing bundle payload: %s", postJSON)
	}
	if resp == nil || len(resp.JSON) == 0 {
		t.Fatal("expected transaction response JSON")
	}
	if postJSON != string(resp.JSON) {
		t.Fatalf("post-commit JSON does not match response envelope")
	}
}

func newHookedService(t *testing.T, reg *hooks.Registry) *core.ResourceService {
	t.Helper()
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
	return svc
}
