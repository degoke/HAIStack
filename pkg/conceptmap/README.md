# haistack-conceptmap (`pkg/conceptmap`)

FHIR **ConceptMap** resolution and **coding translation** for StructureMap `translate()` and related tooling.

Library only — not a standalone terminology server.

---

## What it does

**`Translator`** (`translate.go`):

```go
type TranslateRequest struct {
    MapCanonical string
    Source       map[string]any // Coding-shaped map
    TargetSystem string         // optional group filter
}
```

Flow:

1. `Translate` / `TranslateResolved` resolve `Map` via `Resolver`
2. `translateLocal` matches groups (`Source`, `Target`, `element.Code`)
3. Apply targets; filter by equivalence; handle `NoMap` and `Unmapped` modes
4. If local not-found and `Remote` set → `RemoteTranslateClient.Translate`

**Resolvers** (`resolver.go`, `chain.go`):

| Implementation | Use |
|----------------|-----|
| `StoreResolver` | ConceptMap resources + DefinitionStore fallback, versioned canonicals |
| `StaticResolver` | Tests and fixtures |
| `ChainResolver` | Ordered fallback |

**Error helpers:** `IsNotFound`, `IsNoMap`, `IsNoTranslation`.

**Parsing:** `ParseMap`, `ResolveEnvelope`, `Canonical(Map)`.

Does **not** replace [`pkg/terminology`](../terminology/README.md) for ValueSet expansion or validation bindings.

---

## How it fits in the ecosystem

```text
ConceptMap (FHIR in store)
        │
        ▼
Resolver ──► Map ──► Translator.Translate ──► []map[string]any (codings)
        │                      │
        │                      └──► structuremap.Engine (translate transform)
        └── optional RemoteTranslateClient
```

Runtime attaches a store-backed `Translator` to StructureMap extraction (`wire.go`).

---

## When to use it

- Test or implement **StructureMap `translate`**
- Deterministic coding maps without hitting TX on every line
- Custom StructureMap runners

Use **`pkg/terminology`** for `$expand`, `$validate-code`, CodeSystem lookup at validation time.

Most apps import this only indirectly through **`pkg/structuremap`**.

---

## Usage modes

### 1. StructureMap engine (primary)

```go
engine := structuremap.Engine{
    Translator: conceptmap.Translator{
        Resolver: &conceptmap.StoreResolver{Resources: rs, Registry: defs},
        Remote:   remoteClient, // optional
    },
}
```

### 2. Direct translation (tests)

From `translate_test.go`:

```go
m := conceptmap.Map{
    URL: "http://example.org/maps/gender",
    Group: []conceptmap.Group{{
        Source: "http://example.org/source",
        Target: "http://example.org/target",
        Element: []conceptmap.Element{{
            Code: "M",
            Target: []conceptmap.Target{{Code: "male", Equivalence: "equivalent"}},
        }},
    }},
}
tr := conceptmap.Translator{Resolver: conceptmap.StaticResolver{m.URL: m}}
codings, err := tr.Translate(ctx, conceptmap.TranslateRequest{
    MapCanonical: m.URL,
    Source: map[string]any{"system": "http://example.org/source", "code": "M"},
})
```

`TranslateResolved` returns the `Map` used for provenance.

### 3. Store-backed production resolver

Install ConceptMap FHIR resources in the repository. `StoreResolver` lists `ConceptMap` ids on each resolve so new maps appear without restart.

### 4. Chain resolvers

```go
conceptmap.ChainResolver{Resolvers: []conceptmap.Resolver{static, store}}
```

### 5. Remote fallback

Implement `RemoteTranslateClient` — see `remote_translate.go`. Local success never calls remote.

---

## Examples (APIs from this repo)

**No-map** (`TestTranslatorRejectsNoMap`): `NoMap: true` → `IsNoMap(err)`.

**Unmapped** (`translate_test.go`):

- `Mode: "provided"` — pass source code/system
- `Mode: "use-source-code"` — code with group target system
- `Mode: "fixed"` — constant target

**StructureMap** (`structuremap/transforms_test.go`):

```go
engine.applyTransform(ctx, "translate", []Parameter{{ValueString: m.URL}}, nil,
    map[string]any{"system": "...", "code": "F"})
```

**Two-parameter translate** — `TestTranslateTransformTwoParameterForm`.

**Deduping** — multiple targets dedupe by system/code/display.

---

## Where it fits

```text
ConceptMap ──► pkg/conceptmap ──► translate() in pkg/structuremap
```

Same ConceptMap resources may also feed terminology projections elsewhere.

---

## Limits

- Executes declared mappings only — no clinical inference
- Remote optional; resolver required for local-first setups
- Not TX server — no closure, subsumption, or expand
- Store list cap 10k ConceptMap ids per index build
- Disjoint/unmatched equivalences skipped; empty matches → `IsNoTranslation`
- Ambiguous version duplicates → error from `resolveFromIndex`

---

## Related docs

- [pkg/structuremap/README.md](../structuremap/README.md)
- [pkg/terminology/README.md](../terminology/README.md)
- [pkg/sdc/README.md](../sdc/README.md)
- [HL7 ConceptMap](https://hl7.org/fhir/conceptmap.html)
