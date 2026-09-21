package hooks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/hooks"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestRegistryRunsInOrderAndStopsOnError(t *testing.T) {
	reg := hooks.NewRegistry()
	var order []string
	if err := reg.On(hooks.Incoming, func(context.Context, *hooks.Event) error {
		order = append(order, "a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.On(hooks.Incoming, func(context.Context, *hooks.Event) error {
		order = append(order, "b")
		return errors.New("stop")
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.On(hooks.Incoming, func(context.Context, *hooks.Event) error {
		order = append(order, "c")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := reg.Run(context.Background(), hooks.Incoming, &hooks.Event{Action: hooks.ActionRead, ResourceType: "Patient"})
	if err == nil || err.Error() != "stop" {
		t.Fatalf("Run error = %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
}

func TestRegistryRejectsUnknownPoint(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On("storage-precommit", func(context.Context, *hooks.Event) error { return nil }); err == nil {
		t.Fatal("expected unknown point error")
	}
}

func TestRegistryMutatesEventResource(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
		event.Resource = &types.ResourceEnvelope{ResourceType: "Patient", ID: "mutated"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	event := &hooks.Event{Resource: &types.ResourceEnvelope{ResourceType: "Patient", ID: "orig"}}
	if err := reg.Run(context.Background(), hooks.PreStorage, event); err != nil {
		t.Fatal(err)
	}
	if event.Resource.ID != "mutated" {
		t.Fatalf("resource id = %q", event.Resource.ID)
	}
}

func TestNilRegistryRunIsNoop(t *testing.T) {
	var reg *hooks.Registry
	if err := reg.Run(context.Background(), hooks.Outgoing, &hooks.Event{}); err != nil {
		t.Fatal(err)
	}
}
