# HAIStack policy DSL semantics

This document is the normative description of how `pkg/auth` and `pkg/smart`
compose. It is the research artefact for Track C (issue #11).

## Layers

Authorization is **not** OAuth token exchange. A valid SMART token is an
identity + scope grant. Access is decided afterwards:

```
token valid
  ∧ SMART.ScopeImplies(actor, resourceType, verb)
  ∧ auth.Engine decision (catalog + policy DSL + patient overlay)
```

Policy may only **narrow** SMART scopes. A policy allow never expands a
missing scope. This matches the SMART warning that servers SHOULD apply
additional constraints (patient compartment, business rules) on top of
granted scopes.

## Decision algorithm (`pkg/auth.Engine`)

For resource read/write, view execution, and AI tools:

1. **Reject malformed requests** — principal id/kind and tenant id are required.
2. **Tenant binding** — the principal must be bound to the requested tenant
   (users). Cross-tenant requests are denied.
3. **Patient-scope overlay** — when `TenantContext.PatientScope` is set,
   `Patient/{id}` must equal that id. Other resource types use a
   patient-compartment resolver when one is configured
   (`CheckEnvelopePatientScope`). Mismatch is deny.
4. **Required permissions** — SMART adapters set `RequiredPermissions` from
   granted scopes (`resourceType.verb`, or `*.verb`) on read, write, view,
   and AI-tool requests (`ToReadRequest`, `ToWriteRequest`, `ToViewRequest`,
   `ToAIToolRequest`). The principal must hold those permissions via
   catalog roles. Missing permissions are deny.
5. **Policy DSL** — `CompiledPolicy.Evaluate`:
   - Rules are evaluated **in document order**.
   - The **first matching rule wins**.
   - Empty match fields mean "any".
   - `*` in a string list matches any value for that field.
   - If no rule matches, `defaultEffect` applies (**deny** when omitted).
6. Result: `Decision{Allowed, Reason, RequiredPermissions, ...}`.

The Track C YAML runner does **not** load a ViewDefinition. View and AI
cases evaluate SMART-derived `RequiredPermissions` on the request (view
uses `checkAnyRequiredPermission` on those values). A production view
executor may also require a ViewDefinition's declared permissions; that
gate is outside this catalogue.

Device push (`push-device-event`) and module install (`install-module`) are
separate `pkg/auth` engine paths (trusted device, then policy). The Track C
YAML runner does not accept those actions; they are not catalogue cases.

## Policy DSL match fields

| Field | Meaning |
|-------|---------|
| `principalKinds` | `user`, `device`, `service`, `ai-agent` |
| `tenants` | tenant ids (`*` wildcard) |
| `roles` | any overlapping role |
| `anyPermissions` / `allPermissions` | permission checks |
| `resourceTypes` | FHIR resource type |
| `actions` | `read`, `write`, `execute-view`, `execute-ai-tool`, … |
| `viewNames` / `toolNames` / `moduleNames` | named targets |
| `purposeOfUse` | attribute match |
| `deviceTrusted` / `deviceStatuses` | device trust |
| `patientScoped` | whether tenant patient scope is set |

Permissions treat `appointment.read` and `read-appointment` as equivalent.

## SMART adapter

`pkg/smart.AuthAdapter` turns token claims + launch context into:

- `Principal` (id, kind, tenant role bindings)
- `TenantContext` (`PatientScope` from `launch/patient` / `patient` claim)
- `Permissions` derived from resource scopes (`Observation.read`, `*.read`, …)
- `RequiredPermissions` on read, write, view, and AI-tool requests (`requiredFor`)

`ScopeImplies` is the scope-layer check used by the research YAML runner.
The intersection is computed as:

```
allowed = ScopeImplies(resource, verb) && engine.Can*(...).Allowed
```

## Worked examples

The machine-readable catalogue is [`scenarios.yaml`](scenarios.yaml).
Narrative copies of all fourteen cases. Where `user/*.read` would require
clinician `*.read` for SMART `RequiredPermissions`, the YAML overlays that
permission (`policyRoleGrants` on `observation-only`, or per-scenario
`roleGrants`) so the interesting gate is still scope ∩ policy:

1. **Broad scope ∩ matching policy (allow).** `user/*.read` and
   observation-only (with clinician `*.read` overlay) → Observation/obs-1
   is allowed.
2. **Broad scope ∩ narrowing policy (deny).** Same token and overlay but
   observation-only policy → Appointment/a1 is denied. Scopes granted more
   than policy permits; policy wins (narrowing).
3. **Narrow scope ∩ broad policy (deny).** `user/Patient.read` and
   allow-all-read policy → Observation/obs-1 is denied. Policy cannot
   expand missing Observation scope.
4. **Matching granular scope ∩ matching policy (allow).**
   `user/Observation.rs` ∩ observation-only policy → Observation allowed.
5. **Write scope missing (deny).** `user/Observation.read` ∩ allow-all
   policy → Observation write denied (no write verb).
6. **Patient overlay allow.** `launch/patient user/Patient.read` with
   `patient=pat-1` reading Patient/pat-1 → allow (base policy patient-read).
7. **Patient overlay deny.** Same token reading Patient/pat-2 → deny
   (compartment mismatch) even though scopes grant Patient read.
8. **Deny-by-default.** `user/*.read` ∩ base policy reading
   MedicationRequest → deny (no matching rule). Clinician `*.read` is
   granted for this case only so deny is unmatched policy, not missing
   RequiredPermissions.
9. **First-match deny wins.** Deny-first policy lists a deny Appointment
   rule before an allow rule → Appointment read denied (same `*.read`
   overlay as example 8).
10. **Cross-tenant deny.** Clinician bound to `tenant-a` requesting
    `tenant-b` → deny (tenant binding), regardless of scopes.
11. **View execution.** `user/*.read` ∩ base policy executing
    `patient_summary_view` → allow after clinician `*.read` overlay so
    `ToViewRequest` RequiredPermissions are satisfied.
12. **AI tool.** `user/*.read` ∩ base policy `execute-ai-tool` `run_view`
    → allow (same overlay; `ToAIToolRequest` RequiredPermissions).
13. **Backend system scope allow.** `system/*.read` on a service principal ∩
    observation-only → Observation allow.
14. **Backend system scope deny.** Same token ∩ observation-only →
    Appointment deny (policy narrowing). Patient-compartment overlay is
    examples 6–7 (`action: read` plus `patientId`), not a separate
    `patient-access` catalogue case.

## Consent overlay (not a second engine)

R4 `Consent` (and future R5/R6 `Permission`) are **inputs** that a host
application can compile into the same policy DSL and/or patient-scope
overlay. HAIStack v1 does not interpret Consent resources automatically.
`scenarios.yaml` has no consent-state field; it is SMART scopes ∩ policy
only. See [consent-patterns.md](consent-patterns.md).

## Conformance target

These scenarios are intended as an open test suite. A non-HAIStack
implementation can emit the same YAML decisions without speaking Go.
