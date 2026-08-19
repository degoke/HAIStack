# haistack-fhirpath (`pkg/fhirpath`)

In-memory FHIRPath evaluation for a **single FHIR resource at a time**.

## What it does

[FHIRPath](https://build.fhir.org/ig/HL7/FHIRPath/) is a small query language for FHIR — like XPath for XML. You write expressions such as:

- `Patient.name.given` — given names
- `Patient.telecom.where(system = 'phone').value` — phone numbers
- `Observation.value.ofType(Quantity).value` — a numeric observation value

This package **runs those expressions** against a Patient, Observation, or other resource you already have in memory. It does **not** search a database or fetch resources; it only reads fields from a resource you pass in.

**Given this one FHIR resource, what values does this path return?**

## Inputs

- `*types.ResourceEnvelope` (typical haistack container), or
- A Google FHIR R4 proto (e.g. `*patient_go_proto.Patient`)

Raw JSON bytes and generic maps are not accepted.

## Usage

```go
eng, err := fhirpath.NewEngine(fhirpath.Config{})
ctx := context.Background()

// Full result collection
values, err := eng.Eval(ctx, "Patient.name.given", envelope)

// Single string or bool (must be exactly one matching value)
name, err := eng.EvalString(ctx, "Patient.name.given", envelope)
exists, err := eng.EvalBool(ctx, "Patient.name.exists()", envelope)
```

Reuse a compiled expression in a loop:

```go
compiled, _ := eng.Compile("Patient.telecom.where(system = 'phone').value")
for _, env := range patients {
    phones, _ := compiled.Eval(ctx, env)
    // ...
}
```

## Where it fits

| Layer | Role |
|-------|------|
| **Search** | Find which resources match (`name=Smith`) |
| **fhirpath** | Read fields inside a resource you already have |
| **View** | Build structured projections from resources |

Future uses: search indexing, ViewDefinitions, AI tool preconditions.

## MVP limits

- One resource per evaluation
- Google FHIR R4 only
- No `resolve()` or terminology (`memberOf()`)
- String functions such as `contains()` on FHIR strings are supported
- `*types.ResourceEnvelope` with JSON populated is a valid input (proto optional)
- Optional custom functions at engine creation

See [doc.go](./doc.go) for the full API and package boundaries.
