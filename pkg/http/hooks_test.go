package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/hooks"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestIncomingHookRejectsRequest(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.Incoming, func(_ context.Context, event *hooks.Event) error {
		if event.Action == hooks.ActionRead && event.ResourceType == "Patient" {
			return errors.New("no patients")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(context.Context, string, string) (*types.ResourceEnvelope, error) {
				t.Fatal("read should not run")
				return nil, nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOutgoingHookMutatesResponse(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		if event.Resource == nil {
			return errors.New("missing resource")
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Resource.JSON, &payload); err != nil {
			return err
		}
		payload["active"] = true
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		event.Resource.JSON = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, _, id string) (*types.ResourceEnvelope, error) {
				return patientEnvelope(id, "Doe"), nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"active":true`) {
		t.Fatalf("outgoing mutation missing: %s", rec.Body.String())
	}
}
