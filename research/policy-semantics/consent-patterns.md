# Consent resource patterns

HAIStack v1 does not evaluate FHIR Consent or Permission resources inside
`pkg/auth`. Host applications compile consent state into the same
**policy DSL + patient-scope overlay** described in
[SEMANTICS.md](SEMANTICS.md). This note records vendor-neutral patterns so
implementations can share test cases.

## R4 Consent (permit / deny)

Typical permit for a patient-facing app reading Observations:

```json
{
  "resourceType": "Consent",
  "id": "consent-obs-permit",
  "status": "active",
  "scope": {
    "coding": [{
      "system": "http://terminology.hl7.org/CodeSystem/consentscope",
      "code": "patient-privacy"
    }]
  },
  "category": [{
    "coding": [{
      "system": "http://terminology.hl7.org/CodeSystem/consentcategorycodes",
      "code": "npp"
    }]
  }],
  "patient": { "reference": "Patient/pat-1" },
  "provision": {
    "type": "permit",
    "actor": [{
      "role": {
        "coding": [{
          "system": "http://terminology.hl7.org/CodeSystem/v3-ParticipationType",
          "code": "IRCP"
        }]
      },
      "reference": { "reference": "Organization/clinic-a" }
    }],
    "action": [{
      "coding": [{
        "system": "http://terminology.hl7.org/CodeSystem/consentaction",
        "code": "access"
      }]
    }],
    "class": [{
      "system": "http://hl7.org/fhir/resource-types",
      "code": "Observation"
    }]
  }
}
```

**Compile to HAIStack:** set `TenantContext.PatientScope = "pat-1"` and add
an allow rule matching `resourceTypes: ["Observation"]` for that
principal/app. A deny provision becomes a first-match deny rule (see
example 9 in SEMANTICS.md).

## Deny provision (break-glass still policy)

A deny provision for MedicationRequest is compiled as an ordered deny
rule **before** any broader allow. HAIStack has no break-glass workflow in
v1; a host can model break-glass as a later allow rule with
`purposeOfUse: ["break-glass"]` plus audit.

## Future R5/R6 Permission

R5 `Permission` (and R6 refinements) replace some Consent.provision
nesting with explicit `combining` and `rule`. Issue #11 sketched a host
tuple of `principal + consent/permission state + request`. This catalogue
does **not** store consent state. Compile Consent or Permission into the
policy DSL and/or patient-scope overlay, then assert the compiled YAML
(`principal + scopes + policy + request` in [`scenarios.yaml`](scenarios.yaml)).

Until `pkg/auth` grows a Consent compiler, tests should keep compiling to
those YAML scenarios rather than interpreting Consent or Permission JSON
inside the engine.

## Cross-vendor note

These JSON examples are FHIR resources, not HAIStack-specific. A HAPI or
Firely authorization layer can compile the same Consent instances into its
own policy or overlay and replay the **compiled** tuples in
`scenarios.yaml` (SMART scopes ∩ policy). It cannot ingest the Consent
JSON into `scenarios.yaml` as-is: that file has no consent-state field.
