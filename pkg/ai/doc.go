// Package ai implements haistack-ai, a policy-governed FHIR AI gateway for Health
// AI Stack.
//
// # Scope
//
// v1 centers on four generic, policy-bounded tools rather than a large catalog of
// hard-coded domain tools:
//
//   - read_fhir_resource
//   - search_fhir_resources
//   - run_view
//   - write_fhir_resource
//
// These are constrained, typed, audited operations in front of pkg/view,
// pkg/search, pkg/core, and pkg/validate — not raw FHIR server passthroughs.
// There is no arbitrary URL execution, no raw PATCH, and no "submit any FHIR JSON"
// write path in v1.
//
// Optional convenience wrappers (get_patient_summary, get_upcoming_appointments,
// search_patient_by_phone) delegate to the generic core and are registered in
// Registry by default.
//
// # Public API
//
// The package centers on:
//
//   - PolicyEngine / AllowListPolicy: central allow-list and safety layer with
//     CheckRead, CheckSearch, CheckView, and CheckWrite.
//   - Registry: convenience wrappers and custom tool registration.
//   - Executor: validates requests, enforces policy, invokes backing packages,
//     builds citations, and emits audit records via ExecuteTool.
//   - GenericToolDescriptors / Registry.AllToolDescriptors: model-facing tool
//     discovery metadata.
//   - ContextFormatter: converts tool output into model-facing JSON context.
//   - CitationBuilder: attaches provenance from resource refs, search params,
//     view columns, and write metadata.
//   - ModelRouter / Executor.InvokeModel: optional local/cloud model adapter
//     selection; tool execution remains useful without any model configured.
//   - ApprovalHook: optional human approval seam for policy-gated writes.
//   - Deidentifier: optional output scrubbing seam; pass-through by default.
//
// # Tool input shapes
//
// read_fhir_resource: resourceType, id
//
// search_fhir_resources: resourceType, params, optional count and offset
//
// run_view: viewName, optional version, parameters, limit, offset
//
// write_fhir_resource: operation (create|update), resourceType, id (update
// only), fields (approved top-level FHIR fields only)
//
// # Typical usage
//
//	policy := ai.NewAllowListPolicy()
//	policy.Read["Patient"] = ai.ReadTypePolicy{}
//	policy.Search["Patient"] = ai.SearchTypePolicy{
//	    AllowedParams: []string{"name", "telecom"},
//	    MaxCount:      50,
//	}
//	policy.Views["patient_summary_view"] = ai.ViewTypePolicy{}
//	policy.Write["Patient"] = ai.WriteTypePolicy{
//	    CreateFields: []string{"name", "gender"},
//	    UpdateFields: []string{"name"},
//	}
//
//	exec, err := ai.NewExecutor(ai.Config{
//	    Resources: resourceStore,
//	    Search:    searchSvc,
//	    Views:     viewExec,
//	    Core:      coreSvc,
//	    Policy:    policy,
//	    Audit:     ai.AuditStoreAdapter{Store: auditStore},
//	})
//
//	res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
//	    ToolName: ai.ToolReadFhirResource,
//	    Actor:    "agent-1",
//	    Input: map[string]any{
//	        "resourceType": "Patient",
//	        "id":           "pat-1",
//	    },
//	})
//
// # Execution model
//
// ExecuteTool resolves convenience wrappers through Registry, validates typed
// input, runs the matching PolicyEngine check, invokes the backing package,
// optionally de-identifies output, formats model context, builds citations, and
// writes audit records on success, denial, validation failure, and
// approval-required outcomes.
//
// Write operations validate structured field input, may require approval through
// ApprovalHook when policy demands it, and commit through core.ResourceService
// only after policy and validation pass.
//
// # Authorization and audit
//
// PolicyEngine is required. AllowListPolicy denies by default until explicit
// allow-list entries are configured. AuditLogger is optional; when configured it
// records actor, subject, tool name, outcome, and request scope.
//
// # Integration points
//
//   - haistack-view: run_view delegates to view.Executor for structured rows.
//   - haistack-search: search_fhir_resources delegates to search.Service.
//   - haistack-core: write_fhir_resource commits through core.ResourceService.
//   - haistack-validate: optional Validator on the write path.
//   - haistack-auth: future auth subsystem can replace or augment PolicyEngine.
//   - haistack-modules: future installers can register tools and views by name.
package ai
