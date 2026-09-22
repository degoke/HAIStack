# HAIStack overview

HAIStack is a **modular Go toolkit** for building **FHIR-native health data infrastructure** with **safe AI access**. It is not a single monolithic FHIR server. You import the libraries you need and compose local runtimes, edge servers, sync nodes, analytics pipelines, and AI gateways.

- **Repository:** [github.com/degoke/HAIStack](https://github.com/degoke/HAIStack)  
- **Go module:** `github.com/degoke/haistack`  
- **Go version:** 1.26+  
- **License:** [Apache 2.0](../LICENSE)

The module path is lowercase (Go convention). It resolves against the `HAIStack` GitHub repository; you do not need to rename the repo for `go get` to work.

## Why it exists

Healthcare systems are often built as always-online, centralized platforms. That model breaks when:

- Connectivity is unreliable or expensive  
- Clinics need **local-first** workflows that keep working offline  
- Field workers capture data without continuous cloud access  
- Hospitals require **on-premise** control of data and audit trails  
- AI tools need **structured, permissioned** access—not raw database dumps  

FHIR standardizes the **data model**, but production systems still need storage, search, sync, history, validation, authorization, terminology, analytics, and AI-safe boundaries. HAIStack supplies those as **composable libraries** with clear interfaces.

## The story in one sentence

> What if a clinic, mobile app, or edge deployment could keep working when the cloud is unavailable, while still using FHIR as the foundation—and still expose safe, auditable paths for AI and reporting?

Target deployments include **on-device**, **offline-first**, **edge**, **on-premise**, **cloud**, and **AI-assisted** systems. The goal is reusable building blocks developers assemble into their own products—not a replacement for every existing FHIR server.

## Core ideas

### Modular, not monolithic

Use only what you need: SQLite storage alone, FHIRPath alone, a full edge stack with Postgres, or an AI gateway in front of an existing server via `pkg/client`.

### Offline-first

Create, update, search, and store locally. Push to an edge or cloud **hub** when connectivity returns. Sync is **Git-inspired**: local writes are provisional; the hub holds canonical history; push/pull and conflict resolution merge resource-aware changes.

### Canonical FHIR JSON

Resources flow as [`ResourceEnvelope`](../pkg/types/README.md) values (type, id, version, normalized JSON, hash). Business rules live in [`pkg/core`](../pkg/core/README.md). Persistence swaps via [`pkg/store`](../pkg/store/README.md) and [`pkg/sqlite`](../pkg/sqlite/README.md) / [`pkg/postgres`](../pkg/postgres/README.md).

### Terminology as projections

`CodeSystem`, `ValueSet`, and `ConceptMap` remain ordinary FHIR resources. [`pkg/terminology`](../pkg/terminology/README.md) compiles rebuildable, tenant-scoped projections for fast lookup and finite ValueSet expansion. Terminology-aware validation is **opt-in**.

### Safe AI access

AI agents call **typed, policy-governed tools** ([`pkg/ai`](../pkg/ai/README.md))—FHIRPath, bounded search, ViewDefinitions—not arbitrary REST. Decisions and tool use can be recorded via [`pkg/audit`](../pkg/audit/README.md).

### Postgres-first edge

One Go binary plus one Postgres database can hold resources, history, search indexes, sync events, blobs, views, jobs, and audit logs. External object storage, OpenSearch, or queues are **optional** for cloud scale, not required at the edge.

## What is in the repo

| Area | Path | Purpose |
|------|------|---------|
| Libraries | `pkg/*` | Go packages (see [package index](README.md#package-reference)) |
| CLI | `cmd/haistack` | Developer/operator CLI |
| Capability modules | `modules/*` | Example `module.json` bundles (core, SDC, scheduling, …) |
| Examples | `examples/*` | Runnable composition samples |
| Conformance | `conformance/` | FSH, IG build, validator fixtures |
| Research | `research/` | Synthetic benchmarks and evaluation artefacts |
| Docs | `docs/` | Architecture and guides (this tree) |

## Project status

Early-stage, under active development. The [root README](../README.md#project-status) table lists package maturity. Prefer pinning a release tag once you depend on HAIStack in production; until then, track `main` or a known good commit.

## Next steps

- Pick a deployment shape: [Composition patterns](composition-patterns.md)  
- Understand layers and write paths: [Architecture](architecture.md)  
- Run an example: [examples/README.md](../examples/README.md)  
- Browse package docs: [Package reference](README.md#package-reference)
