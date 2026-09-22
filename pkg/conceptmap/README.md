# haistack-conceptmap (`pkg/conceptmap`)

FHIR **ConceptMap** resolution and **coding translation** for StructureMap `translate()` transforms.

## What it does

`pkg/conceptmap` loads ConceptMap resources (from your resolver or store), selects applicable **groups**, and translates source codings to target codings. It is a supporting library for [`pkg/structuremap`](../structuremap/README.md), not a standalone terminology server.

It does **not** replace [`pkg/terminology`](../terminology/README.md) for CodeSystem lookup, ValueSet expansion, or validation-time binding checks.

## When to use it

- StructureMap rules that call **`translate`** with a ConceptMap canonical URL  
- Unit tests that need deterministic map groups without a full TX server  
- Pipelines that already store ConceptMap as FHIR resources in the same repository  

For general code validation against ValueSets, use `terminology` validation integration instead.

## Usage

ConceptMap access is typically wired into a StructureMap engine resolver:

```go
import "github.com/degoke/haistack/pkg/conceptmap"

// Resolver loads ConceptMap JSON by url/id; Translator applies group rules.
```

See `pkg/structuremap` tests and SDC extraction examples for end-to-end usage—callers rarely import `conceptmap` directly except when building custom StructureMap runners.

## Where it fits

```text
ConceptMap (FHIR resource) ──► pkg/conceptmap ──► translate() in pkg/structuremap
```

Terminology projections in `pkg/terminology` handle a different problem space (fast lookup/expand), though both may consume the same canonical ConceptMap resources in your database.

## Limits

- Map quality and completeness are your responsibility—this package executes declared groups, it does not infer mappings.  
- Remote terminology services are out of scope; maps must be available to the resolver you provide.

## Related docs

- [pkg/structuremap/README.md](../structuremap/README.md)  
- [pkg/terminology/README.md](../terminology/README.md)
