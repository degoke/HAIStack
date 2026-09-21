# Policy and consent decision semantics

This document is the citable description of how HAIStack combines SMART
scopes, the `pkg/auth` policy DSL, patient-compartment overlay, and (in
research) R4 Consent provisions. Production `pkg/auth` does **not** yet
evaluate Consent resources; the consent overlay is specified here so
implementations and test suites can share a vendor-neutral algorithm.

## Decision algorithm

Evaluate gates in order. The first failing gate denies. All gates must pass
for an allow.

```
1. Identity        Principal must exist and be bound to the request tenant.
2. Patient overlay If TenantContext.PatientScope is set, the target patient
                   id must equal that scope (Patient.id, or the compartment
                   patient of the resource). Empty PatientScope is unrestricted.
3. SMART scopes    If the request carries SMART scopes, the scope set must
                   grant the resource type and verb for the principal's actor
                   class (patient | user | system). Missing scopes on a
                   policy-only principal skip this gate.
4. Consent overlay If an active Consent provision applies to the patient,
                   resource class, and action:
                     deny provision  → deny
                     permit provision → continue
                   Inactive / unmatched Consent is ignored.
                   Production engines that do not implement Consent skip this
                   gate; research scenarios that declare consent require it.
5. Policy DSL      Compile PolicyDocument. Walk rules in document order.
                   First matching rule wins (allow or deny).
                   If no rule matches, DefaultEffect applies (deny).
```

The research runner applies gates 1–5 in that order (identity, patient overlay,
SMART, consent, policy).

The compact form used in SMART discussions:

```
allowed = scope_grants ∩ policy_allows ∩ consent_permits ∩ patient_scope_ok
```

Policy **narrows** scopes: a `patient/*.read` token is not a read grant for
every resource type if the policy only allows Observation. This matches the
SMART warning that scopes are necessary but not sufficient.

### Policy DSL evaluation (pkg/auth)

- Rules are ordered. First match wins.
- Empty match lists mean "any".
- `*` wildcards are allowed in string lists (tenants, resource types, …).
- Permissions treat `appointment.read` and `read-appointment` as equivalent.
- Device push additionally requires a registered, trusted, active device.
- View execution additionally requires the principal to hold at least one
  permission declared on the ViewDefinition, when that list is non-empty.

### SMART scope derivation

`pkg/smart.AuthAdapter` maps resource scopes to permissions
`{ResourceType}.{verb}` (or `*.{verb}`). `ToReadRequest` sets
`RequiredPermissions` from those derived permissions. If the scope set does
not include the verb, the required-permission check fails **before** policy
allow rules are considered — another form of intersection.

### Research runner overlay (not production)

The catalogue runner grants the clinician role `*.read` **only when a
scenario carries SMART scopes**. That is required so a `patient/*.read`
token can satisfy `RequiredPermissions` (`*.read`) set by
`pkg/smart.AuthAdapter.ToReadRequest`. Without the overlay, wildcard SMART
examples fail the permission check before policy rules run.

This overlay is **research-layer wiring**, not how production `pkg/auth`
intersects SMART:

- Production principals hold only the permissions their roles declare.
- SMART scopes still must imply the verb (`scope_grants`).
- Policy still narrows those grants (`policy_allows`).

The compact production equation remains `scope_grants ∩ policy_allows ∩
consent_permits ∩ patient_scope_ok`. The extra `*.read` is not part of that
intersection; it only makes wildcard SMART cases executable in this runner.
Policy-only scenarios (no `scopes` field) do not receive the overlay.

## Worked examples

Each example has a matching id in `testdata/scenarios.json`.

### 1. `deny_by_default_unmatched_resource`

Clinician, base policy (Appointment + Patient allow rules only), no SMART
scopes. Request: `read MedicationRequest/rx-1`.

**Result: deny.** No rule matches. Default effect is deny.

### 2. `first_match_deny_wins`

Policy: (1) deny Appointment read, (2) allow Appointment read. Request:
`read Appointment/a1`.

**Result: deny.** First matching rule is deny, even though a later allow
would match. Ordering is part of the semantics.

### 3. `first_match_allow`

Base policy, clinician, `read Appointment/a1`.

**Result: allow.** Rule `appointment-rw` matches first.

### 4. `patient_scope_same_patient`

PatientScope=`pat-1`, request `read Patient/pat-1`.

**Result: allow.** Overlay matches; patient-read rule allows.

### 5. `patient_scope_other_patient`

PatientScope=`pat-1`, request `read Patient/pat-2`.

**Result: deny.** Overlay fails before policy. Reason mentions the scoped
patient id.

### 6. `smart_scope_allows_policy_denies`

SMART `patient/*.read` (grants every resource type) + Observation-only
policy. Request: `read Appointment/a1` for launch patient `pat-1`.

**Result: deny.** Scopes grant Appointment.read; policy does not. This is
policy narrowing of SMART scopes.

### 7. `smart_scope_and_policy_allow_observation`

Same token and Observation-only policy. Request: `read Observation/obs-1`.

**Result: allow.** Both gates pass.

### 8. `smart_scope_denies_policy_would_allow`

SMART `patient/Observation.read` (no Appointment) + base policy that allows
Appointment. Request: `read Appointment/a1`.

**Result: deny.** `RequiredPermissions` includes `Appointment.read`, which
the scope-derived permission set does not contain. Scopes ∩ policy.

### 9. `smart_scope_denies_write`

SMART `patient/Appointment.read` + base policy that allows Appointment write.
Request: `write Appointment/a1`.

**Result: deny.** SMART write gate (Appointment.read does not grant write).

### 10. `cross_tenant_denied`

Clinician bound to `tenant-a`. Request tenant `tenant-b`.

**Result: deny.** Tenant-binding gate.

### 11. `purpose_of_use_mismatch`

Allow rule requires `purposeOfUse: TREAT`. Request purpose `ETREAT`.

**Result: deny.** Rule does not match; default deny.

### 12. `consent_permit_observation`

Active R4 Consent, provision `permit` on Observation/read for `pat-1`.
SMART `patient/*.read` + Observation-only policy. Request: read Observation.

**Result: allow.** Consent permit applies and does not block.

### 13. `consent_deny_observation`

Same as (12) but provision `type: deny` on Observation.

**Result: deny.** Consent overlay fails even though scopes and policy allow.
This is the research Consent pattern (R5/R6 Permission is future work).

### 14. `ai_tool_run_view_allowed`

Base policy allows `execute-ai-tool` for `run_view`.

**Result: allow.**

### 14. `ai_tool_write_denied`

Request `execute-ai-tool` `write_fhir_resource` with no matching rule.

**Result: deny.**

## Consent resource pattern (R4)

Research scenarios use a simplified R4 Consent:

```json
{
  "resourceType": "Consent",
  "status": "active",
  "patient": { "reference": "Patient/pat-1" },
  "provision": {
    "type": "permit",
    "class": [{ "code": "Observation" }],
    "action": [{ "coding": [{ "code": "access" }] }]
  }
}
```

`access` maps to policy action `read`. A deny provision with the same class
blocks matching reads. Future R5/R6 `Permission` resources should populate
the same `ConsentState` structure so the catalogue stays version-agnostic.

## Relationship to pkg/testkit/authztest

The executable Go catalogue in `authztest.AllScenarios` covers REST, search,
views, AI, sync, and SMART 2.2 filters. Declarative JSON here is the
portable, vendor-neutral subset (principal + scopes + consent + request →
decision) intended for cross-implementation publication.
