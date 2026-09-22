# Package README standard

Every library under `pkg/<name>/` should ship a **README.md** that stands alone for developers importing that package. The [documentation hub](README.md#package-reference) links here; do not rely on the root README for package detail.

## Required sections (in order)

1. **Title** — `# haistack-<name> (\`pkg/<name>\`)`
2. **What it does** — Plain-language purpose; bullet list of responsibilities; explicit **does not** boundaries.
3. **How it fits in the ecosystem** — ASCII or mermaid diagram plus a table of upstream/downstream packages.
4. **When to use it** — Scenarios where this package is the right choice (and when to use something else).
5. **Usage modes** — At least **four** subsections (`### Mode: …`) for distinct integration patterns (embedded, HTTP, programmatic, tests-only, etc.).
6. **Examples** — Runnable Go snippets using **real public APIs** from this repo (verify against `doc.go`, tests, or examples).
7. **Configuration / key types** — When the package has a `Config`, `Service`, or primary interfaces.
8. **Where it fits** — Table mapping related packages to roles.
9. **Limits / MVP notes** — Honest scope boundaries and deferred features.
10. **Related docs** — Links to `docs/`, sibling `pkg/*/README.md`, and operational guides when relevant.

## Quality bar

- **Accurate APIs** — No invented constructors; run `go doc` or read tests before documenting.
- **Ecosystem context** — Every README answers: “If I use only this import, what else do I typically wire?”
- **Multiple paths** — CLI, `runtime.Builder`, and manual wiring should be mentioned when all three exist.
- **`## Limits`** and **`## Related docs`** — use these exact headings (rename legacy `MVP limits`, `Related packages`, etc.).
- **`## How it fits in the ecosystem`** — preferred title; **`## Where it fits`** is accepted by CI as an equivalent ecosystem section.

## Also document

- `cmd/haistack/README.md` — CLI surface (operator docs).
- `pkg/*/doc.go` — Package comment for `go doc`; keep in sync with README intent (not a duplicate of the full README).

When adding a new `pkg/*` directory, add README.md in the **same PR** as the package code.
