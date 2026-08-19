package ai_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/ai"
)

func TestRegistry_GenericAndConvenienceTools(t *testing.T) {
	reg := ai.NewRegistry()
	tools := reg.List()
	if len(tools) < 3 {
		t.Fatalf("expected at least 3 convenience tools, got %d", len(tools))
	}

	all := reg.AllToolDescriptors()
	if len(all) < 7 {
		t.Fatalf("expected generic + convenience descriptors, got %d", len(all))
	}

	spec, err := reg.Resolve(ai.ToolGetPatientSummary)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if spec.Delegate != ai.ToolRunView {
		t.Fatalf("Delegate = %q, want %q", spec.Delegate, ai.ToolRunView)
	}

	_, err = reg.Resolve("unknown_tool")
	if !errors.Is(err, ai.ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestExecutor_RequiresPolicy(t *testing.T) {
	_, err := ai.NewExecutor(ai.Config{})
	if !errors.Is(err, ai.ErrMissingPolicy) {
		t.Fatalf("err = %v, want ErrMissingPolicy", err)
	}
}

func TestExecutor_UsableWithoutModelRouter(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.Context == "" {
		t.Fatal("expected formatted context")
	}
}

func TestReadFhirResource_Allowed(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["id"] != "pat-jane" {
		t.Fatalf("id = %v, want pat-jane", data["id"])
	}
	if len(res.Citations) != 1 || res.Citations[0].Ref != "Patient/pat-jane" {
		t.Fatalf("citations = %#v", res.Citations)
	}
	if len(h.audit.Records()) == 0 || h.audit.Records()[0].Outcome != "success" {
		t.Fatalf("audit = %#v", h.audit.Records())
	}
}

func TestReadFhirResource_BlockedResourceType(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
	if len(h.audit.Records()) == 0 || h.audit.Records()[0].Outcome != "denied" {
		t.Fatalf("audit outcome = %#v", h.audit.Records())
	}
}

func TestSearchFhirResources_AllowedParams(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:       true,
		withSearch:         true,
		allowPatientSearch: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string][]string{
				"name": {"Doe"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	resources, ok := data["resources"].([]any)
	if !ok || len(resources) == 0 {
		t.Fatalf("resources = %#v", data["resources"])
	}
	first, ok := resources[0].(map[string]any)
	if !ok {
		t.Fatalf("resource = %#v", resources[0])
	}
	if _, exposed := first["telecom"]; exposed {
		t.Fatal("search returned telecom without an explicit search field allow-list")
	}
	if len(res.Citations) < 2 {
		t.Fatalf("expected search + resource citations, got %#v", res.Citations)
	}
}

func TestSearchFhirResources_BlockedParams(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:       true,
		withSearch:         true,
		allowPatientSearch: true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string][]string{
				"birthdate": {"1990-01-01"},
			},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestSearchFhirResources_MixedBlockedParamsDenied(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true, withSearch: true, allowPatientSearch: true})
	_, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string][]string{
				"name":      {"Doe"},
				"birthdate": {"1990-01-01"},
			},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want mixed-parameter denial", err)
	}
}

func TestSearchFhirResources_RejectsInvalidPaging(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true, withSearch: true, allowPatientSearch: true})
	_, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Input: map[string]any{
			"resourceType": "Patient",
			"count":        "not-a-number",
		},
	})
	if !errors.Is(err, ai.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestSearchFhirResources_BlockedResourceType(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients: true,
		withSearch:   true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"params": map[string][]string{
				"name": {"Doe"},
			},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestRunView_WithCitations(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:            true,
		withViews:               true,
		allowPatientSummaryView: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolRunView,
		Actor:    "agent-1",
		Input: map[string]any{
			"viewName": "patient_summary_view",
			"version":  "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["viewName"] != "patient_summary_view" {
		t.Fatalf("viewName = %v", data["viewName"])
	}
	foundViewCitation := false
	for _, c := range res.Citations {
		if c.Kind == "view" && c.Detail["viewName"] == "patient_summary_view" {
			foundViewCitation = true
		}
	}
	if !foundViewCitation {
		t.Fatalf("missing view citation: %#v", res.Citations)
	}
}

func TestRunView_PolicyDenied(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients: true,
		withViews:    true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolRunView,
		Actor:    "agent-1",
		Input: map[string]any{
			"viewName": "patient_summary_view",
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestWriteFhirResource_CreateSuccess(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:          true,
		allowPatientWrite: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields": map[string]any{
				"name": []map[string]string{{"family": "NewPatient"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["resourceType"] != "Patient" {
		t.Fatalf("resourceType = %v", data["resourceType"])
	}
	if data["id"] == "" {
		t.Fatal("expected generated id")
	}
	if res.Citations[0].Kind != "write" {
		t.Fatalf("citation = %#v", res.Citations[0])
	}
}

func TestWriteFhirResource_UpdateSuccess(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:      true,
		withCore:          true,
		allowPatientWrite: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "update",
			"resourceType": "Patient",
			"id":           "pat-jane",
			"fields": map[string]any{
				"name": []map[string]string{{"family": "Updated"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["id"] != "pat-jane" {
		t.Fatalf("id = %v", data["id"])
	}
}

func TestWriteFhirResource_BlockedField(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:          true,
		allowPatientWrite: true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields": map[string]any{
				"active": true,
			},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestWriteFhirResource_BlockedResourceType(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Observation",
			"fields": map[string]any{
				"status": "final",
			},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestWriteFhirResource_ValidationFailure(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:          true,
		withValidator:     true,
		allowPatientWrite: true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"id":           "bad id!",
			"fields": map[string]any{
				"name": []map[string]string{{"family": "Bad"}},
			},
		},
	})
	if !errors.Is(err, ai.ErrValidationFailed) {
		t.Fatalf("err = %v, want ErrValidationFailed", err)
	}
	if len(h.audit.Records()) == 0 || h.audit.Records()[0].Outcome != "validation-failed" {
		t.Fatalf("audit = %#v", h.audit.Records())
	}
}

func TestWriteFhirResource_ApprovalRequired(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:              true,
		allowPatientWrite:     true,
		writeRequiresApproval: true,
		approvalGranted:       false,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields": map[string]any{
				"name": []map[string]string{{"family": "Pending"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !res.ApprovalRequired {
		t.Fatal("expected ApprovalRequired")
	}
	if len(h.approval.calls) != 1 {
		t.Fatalf("approval calls = %d, want 1", len(h.approval.calls))
	}
	records := h.audit.Records()
	if len(records) == 0 || records[len(records)-1].Outcome != "approval-required" {
		t.Fatalf("audit = %#v", records)
	}
}

func TestDeidentificationHookInvocation(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	h.policy.Read["Patient"] = ai.ReadTypePolicy{Deidentify: true}
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !h.deid.called {
		t.Fatal("expected deidentifier to be called")
	}
	if len(res.Redactions) == 0 {
		t.Fatal("expected redactions")
	}
}

func TestConvenienceTool_GetPatientSummary(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:            true,
		withViews:               true,
		allowPatientSummaryView: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolGetPatientSummary,
		Actor:    "agent-1",
		Input:    map[string]any{"limit": 10},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["viewName"] != "patient_summary_view" {
		t.Fatalf("viewName = %v", data["viewName"])
	}
}

func TestConvenienceTool_GetUpcomingAppointments(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedAppointments:      true,
		withViews:             true,
		allowAppointmentsView: true,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolGetUpcomingAppointments,
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["viewName"] != "appointment_view" {
		t.Fatalf("viewName = %v", data["viewName"])
	}
}

func TestConvenienceTool_SearchPatientByPhone(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:       true,
		withSearch:         true,
		allowPatientSearch: true,
	})
	ctx := context.Background()

	_, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchPatientByPhone,
		Actor:    "agent-1",
		Input: map[string]any{
			"phone": "555-0100",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
}

func TestModelRouter_SelectsAdapter(t *testing.T) {
	router := &ai.ModelRouter{
		Local: &fakeModelAdapter{name: "local"},
		Cloud: &fakeModelAdapter{name: "cloud"},
	}
	ctx := context.Background()

	localResp, err := router.Invoke(ctx, ai.ModelRequest{Hint: "local", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Invoke local: %v", err)
	}
	if localResp.Adapter != "local" {
		t.Fatalf("adapter = %q, want local", localResp.Adapter)
	}

	cloudResp, err := router.Invoke(ctx, ai.ModelRequest{Hint: "cloud", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Invoke cloud: %v", err)
	}
	if cloudResp.Adapter != "cloud" {
		t.Fatalf("adapter = %q, want cloud", cloudResp.Adapter)
	}
}

func TestPolicyDenialAuditOnSearch(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients: true,
		withSearch:   true,
	})
	ctx := context.Background()

	_, _ = h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"params":       map[string][]string{"name": {"Doe"}},
		},
	})
	if len(h.audit.Records()) == 0 {
		t.Fatal("expected audit record on denial")
	}
}

func TestAllowListPolicy_CheckWriteBlockedField(t *testing.T) {
	policy := ai.NewAllowListPolicy()
	policy.Write["Patient"] = ai.WriteTypePolicy{CreateFields: []string{"name"}}

	_, err := policy.CheckWrite(context.Background(), ai.WritePolicyRequest{
		Operation:    "create",
		ResourceType: "Patient",
		Fields:       map[string]any{"gender": "female"},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestAllowListPolicy_RequiresExactIncludeDirectives(t *testing.T) {
	policy := ai.NewAllowListPolicy()
	policy.Search["Observation"] = ai.SearchTypePolicy{AllowedParams: []string{"_include"}}
	_, err := policy.CheckSearch(context.Background(), ai.SearchPolicyRequest{
		ResourceType: "Observation",
		Params:       url.Values{"_include": {"Observation:subject"}},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want exact-directive denial", err)
	}
	policy.Search["Observation"] = ai.SearchTypePolicy{
		AllowedParams:   []string{"_include"},
		AllowedIncludes: []string{"Observation:subject"},
	}
	decision, err := policy.CheckSearch(context.Background(), ai.SearchPolicyRequest{
		ResourceType: "Observation",
		Params:       url.Values{"_include": {"Observation:subject"}},
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("allowed include = %#v err=%v", decision, err)
	}
}

func TestGenericToolDescriptors(t *testing.T) {
	descriptors := ai.GenericToolDescriptors()
	if len(descriptors) != 4 {
		t.Fatalf("len = %d, want 4", len(descriptors))
	}
	if descriptors[0].Name != ai.ToolReadFhirResource || !descriptors[0].Generic {
		t.Fatalf("first descriptor = %#v", descriptors[0])
	}
}

func TestSearchMaxCountClampedByPolicy(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:       true,
		withSearch:         true,
		allowPatientSearch: true,
	})
	h.policy.Search["Patient"] = ai.SearchTypePolicy{
		AllowedParams: []string{"name"},
		MaxCount:      1,
	}
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"params":       map[string][]string{"name": {"Doe"}},
			"count":        100,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if count, ok := data["count"].(float64); !ok || count > 1 {
		t.Fatalf("count = %v, want <= 1", data["count"])
	}
}

func TestSearchFhirResources_OffsetPaging(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true, withSearch: true, allowPatientSearch: true})
	res, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolSearchFhirResources,
		Input: map[string]any{
			"resourceType": "Patient",
			"params":       map[string][]string{"name": {"Doe"}},
			"count":        1,
			"offset":       1,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	resources, ok := data["resources"].([]any)
	if !ok || len(resources) != 0 {
		t.Fatalf("paged resources = %#v, want empty second page", data["resources"])
	}
}

func TestRunView_DeidentifyWhenPolicyRequires(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:            true,
		withViews:               true,
		allowPatientSummaryView: true,
	})
	h.policy.Views["patient_summary_view"] = ai.ViewTypePolicy{Deidentify: true}
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolRunView,
		Actor:    "agent-1",
		Input: map[string]any{
			"viewName": "patient_summary_view",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !h.deid.called {
		t.Fatal("expected deidentifier to be called for view output")
	}
	if len(res.Redactions) == 0 {
		t.Fatal("expected redactions on view output")
	}
	if h.deid.last.ResourceType != "Patient" || h.deid.last.ViewName != "patient_summary_view" {
		t.Fatalf("deidentify request = %#v, want view identity and resource type", h.deid.last)
	}
}

func TestRunView_ClampsLimitByPolicy(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:            true,
		withViews:               true,
		allowPatientSummaryView: true,
	})
	h.policy.Views["patient_summary_view"] = ai.ViewTypePolicy{MaxCount: 1}
	res, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolRunView,
		Input: map[string]any{
			"viewName": "patient_summary_view",
			"limit":    1000,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	rows, ok := data["rows"].([]any)
	if !ok || len(rows) > 1 {
		t.Fatalf("rows = %#v, want at most one row", data["rows"])
	}
}

func TestExecutor_InvokeModelUsesRouter(t *testing.T) {
	router := &ai.ModelRouter{
		Local: &fakeModelAdapter{name: "local"},
		Cloud: &fakeModelAdapter{name: "cloud"},
	}
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	h.exec, _ = ai.NewExecutor(ai.Config{
		Resources:   h.resources,
		Policy:      h.policy,
		ModelRouter: router,
	})

	resp, err := h.exec.InvokeModel(context.Background(), ai.ToolRequest{ModelHint: "cloud"}, "summarize", "{}")
	if err != nil {
		t.Fatalf("InvokeModel: %v", err)
	}
	if resp == nil || resp.Adapter != "cloud" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := ai.NewRegistry()
	err := reg.Register(ai.ToolSpec{
		Name:     ai.ToolGetPatientSummary,
		Delegate: ai.ToolRunView,
		MapInput: func(input map[string]any) (map[string]any, error) { return input, nil },
	})
	if !errors.Is(err, ai.ErrToolAlreadyRegistered) {
		t.Fatalf("err = %v, want ErrToolAlreadyRegistered", err)
	}
}

func TestApprovalRequiredAuditOutcome(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:              true,
		allowPatientWrite:     true,
		writeRequiresApproval: true,
		approvalGranted:       false,
	})
	ctx := context.Background()

	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields":       map[string]any{"name": []map[string]string{{"family": "Pending"}}},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.AuditMeta.Outcome != "approval-required" {
		t.Fatalf("outcome = %q, want approval-required", res.AuditMeta.Outcome)
	}
}

func TestApprovalStoreRequiresApprovalBeforeCommit(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:              true,
		allowPatientWrite:     true,
		writeRequiresApproval: true,
	})
	store := ai.NewMemoryApprovalStore()
	h.exec, _ = ai.NewExecutor(ai.Config{
		Core:          h.core,
		Policy:        h.policy,
		Audit:         h.audit,
		ApprovalStore: store,
		Now:           h.clock.Now,
	})
	req := ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields":       map[string]any{"name": []map[string]string{{"family": "Pending"}}},
		},
	}
	pending, err := h.exec.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("pending ExecuteTool: %v", err)
	}
	if !pending.ApprovalRequired || pending.ApprovalToken == "" {
		t.Fatalf("pending result = %#v", pending)
	}
	if err := store.Approve(pending.ApprovalToken); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	req.ApprovalToken = pending.ApprovalToken
	committed, err := h.exec.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("approved ExecuteTool: %v", err)
	}
	if committed.ApprovalRequired {
		t.Fatal("approved write remained pending")
	}
	if _, err := h.exec.ExecuteTool(context.Background(), req); !errors.Is(err, ai.ErrApprovalTokenInvalid) {
		t.Fatalf("replay err = %v, want ErrApprovalTokenInvalid", err)
	}
}

func TestNewExecutorRequiresAuditWhenConfigured(t *testing.T) {
	_, err := ai.NewExecutor(ai.Config{Policy: ai.NewAllowListPolicy(), AuditRequired: true})
	if !errors.Is(err, ai.ErrMissingAudit) {
		t.Fatalf("err = %v, want ErrMissingAudit", err)
	}
}

func TestExecutorRequiresConversationIDWhenConfigured(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true, allowPatientRead: true})
	h.exec, _ = ai.NewExecutor(ai.Config{
		Resources:             h.resources,
		Policy:                h.policy,
		Audit:                 h.audit,
		RequireConversationID: true,
		Now:                   h.clock.Now,
	})
	_, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Input:    map[string]any{"resourceType": "Patient", "id": "pat-jane"},
	})
	if !errors.Is(err, ai.ErrMissingConversationID) {
		t.Fatalf("err = %v, want ErrMissingConversationID", err)
	}
}

func TestDeidentificationRequiresExplicitImplementation(t *testing.T) {
	h := newTestHarness(t, harnessOptions{seedPatients: true, allowPatientRead: true})
	h.policy.Read["Patient"] = ai.ReadTypePolicy{Deidentify: true}
	h.exec, _ = ai.NewExecutor(ai.Config{
		Resources: h.resources,
		Policy:    h.policy,
		Audit:     h.audit,
		Now:       h.clock.Now,
	})
	_, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Input:    map[string]any{"resourceType": "Patient", "id": "pat-jane"},
	})
	if !errors.Is(err, ai.ErrMissingDeidentifier) {
		t.Fatalf("err = %v, want ErrMissingDeidentifier", err)
	}
}

func TestWriteRejectsReservedFields(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	_, err := h.exec.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolWriteFhirResource,
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields":       map[string]any{"id": "forged"},
		},
	})
	if !errors.Is(err, ai.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}
