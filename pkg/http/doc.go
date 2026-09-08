// Package http implements haistack-http, the FHIR REST adapter for Health AI Stack.
//
// haistack-http is a thin transport layer over pkg/core, pkg/search, pkg/registry,
// and optional pkg/auth. It owns HTTP routing, request/response translation,
// OperationOutcome rendering, CapabilityStatement metadata, and pluggable auth
// middleware. Resource lifecycle, persistence, indexing, sync, and AI logic remain
// in lower layers.
//
// The handler also exposes unauthenticated /health and /healthz liveness
// probes. Readiness depends on the runtime successfully constructing the
// handler and its backing services.
//
// Import this package with an alias because its name shadows net/http:
//
//	import (
//	    nethttp "net/http"
//	    hahttp "github.com/degoke/health-ai-stack/pkg/http"
//	)
//
// # Design principles
//
// Transport-only adapter:
//
//   - Handlers parse paths, query strings, and bodies into types.ResourceEnvelope
//     or url.Values, then delegate to injected services.
//   - No database, registry, or policy engine imports in handler logic beyond
//     narrow interfaces and optional adapters.
//   - Business rules (validation, versioning, indexing, transactions) stay in
//     pkg/core and pkg/search.
//
// Narrow dependency interfaces:
//
//   - ResourceService — Create, Read, Update, Delete, History, transaction and
//     batch bundle processing, and JSON Patch.
//   - SearchService — SearchBundle.
//   - CapabilitySource — CapabilitySnapshot for /metadata.
//   - SDCService — optional SDC operation adapter for population, validation,
//     assembly, extraction, and adaptive routes.
//   - OperationService — optional generic custom-operation adapter.
//   - AuthChecker — AuthorizeRead, AuthorizeWrite, AuthorizeSearch.
//
// Concrete adapters (CoreResourceService, SearchServiceAdapter,
// RegistryCapabilitySource, PolicyAuthChecker) wire existing services without
// widening their public APIs.
//
// FHIR-first responses:
//
//   - Success and OperationOutcome responses negotiate application/fhir+json or
//     application/fhir+xml from Accept or _format.
//   - Failures map service, search, auth, and rate-limit errors to OperationOutcome
//     resources with appropriate HTTP status codes.
//   - Create responses include Location; read/update responses include ETag and
//     Last-Modified when version metadata is available.
//
// # Public API
//
//   - NewHandler(Config) (net/http.Handler, error) — constructs the FHIR REST
//     handler tree.
//   - Config — BasePath (default /fhir), ResourceService, optional SearchService,
//     CapabilitySource, ServerMetadata, Codec, auth hooks, and RateLimit.
//   - ServerMetadata — software name/version and server description for
//     CapabilityStatement generation.
//   - PrincipalResolver — extracts auth.Principal and auth.TenantContext from a
//     request.
//   - AuthChecker — authorizes read, write, and search before handler dispatch.
//
// # Supported endpoints (MVP)
//
//   - GET    /fhir/metadata                  — CapabilityStatement
//   - POST   /fhir                           — transaction or batch Bundle
//   - GET    /fhir/{ResourceType}            — type-level search (searchset Bundle)
//   - POST   /fhir/{ResourceType}/_search    — POST search
//   - POST   /fhir/{ResourceType}            — create (conditional create via search params / If-None-Exist)
//   - PUT    /fhir/{ResourceType}?...          — conditional update
//   - DELETE /fhir/{ResourceType}?...          — conditional delete
//   - GET    /fhir/{ResourceType}/{id}       — read
//   - PUT    /fhir/{ResourceType}/{id}       — update
//   - PATCH  /fhir/{ResourceType}/{id}       — JSON Patch update
//   - DELETE /fhir/{ResourceType}/{id}       — delete (204 No Content)
//   - GET    /fhir/{ResourceType}/{id}/_history — history Bundle
//   - GET    /fhir/$export                     — bulk export (501 until implemented)
//   - GET    /fhir/Group/{id}/$export          — group bulk export (501 until implemented)
//   - GET/POST /fhir/$operation and resource operation paths — custom operations
//   - POST   /sync/push, GET /sync/pull        — optional sync hub routes via NewRootHandlerWithSyncMiddleware;
//     pull defaults to 100 events and accepts limits from 1 through 1000
//   - POST   /fhir/Questionnaire/$populate, $assemble
//   - POST   /fhir/{ResourceType}/$validate — FHIR Resource/$validate (OperationOutcome)
//   - POST   /fhir/QuestionnaireResponse/$validate, $extract — SDC operations
//   - POST   /fhir/Questionnaire/$next-question, $next, $answer (adaptive adapter)
//
// Routing uses the Go standard library net/http ServeHTTP pattern; no third-party
// router is required.
//
// # Error mapping
//
// Service errors from pkg/core are mapped via core.KindOf and
// core.OperationOutcomeFromError:
//
//   - invalid      → 400 Bad Request
//   - not-found    → 404 Not Found
//   - conflict     → 409 Conflict
//   - not-supported → 400 Bad Request
//   - exception    → 500 Internal Server Error
//
// Search sentinel errors (ErrInvalidQuery, ErrUnknownParam, etc.) → 400.
// Auth denials → 403 Forbidden; missing credentials when auth is enabled → 401.
//
// # Auth
//
// When PrincipalResolver and AuthChecker are both configured, built-in middleware
// resolves identity and authorizes each action before calling services. When auth
// is not configured, requests pass through unchanged.
//
// Alternatively, set AuthMiddleware to wrap the handler with custom logic (for
// example SMART token validation). PolicyAuthChecker adapts auth.PolicyEngine;
// search authorization uses CanReadResource per resource type.
// Sync routes are mounted separately and do not inherit FHIR middleware;
// protect them with RootConfig.SyncMiddleware and use a scoped sync hub.
//
// # Typical usage
//
//	handler, err := hahttp.NewHandler(hahttp.Config{
//	    ResourceService:  hahttp.CoreResourceService{Svc: coreSvc},
//	    SearchService:    hahttp.SearchServiceAdapter{Svc: searchSvc},
//	    CapabilitySource: hahttp.RegistryCapabilitySource{Snapshot: snapshot},
//	    ServerMetadata: hahttp.ServerMetadata{
//	        SoftwareName:    "my-fhir-server",
//	        SoftwareVersion: "1.0.0",
//	    },
//	    PrincipalResolver: myResolver,
//	    AuthChecker:       hahttp.PolicyAuthChecker{Engine: authEngine},
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	nethttp.ListenAndServe(":8080", handler)
//
// # Out of scope (MVP)
//
//   - Bulk export implementation (routes return 501)
//   - Full CapabilityStatement conformance testing
//   - SMART metadata, built-in OAuth2/SMART token runtime
//   - gRPC or non-FHIR content types
//
// See README.md in this directory for endpoint tables, wiring examples, and test
// guidance.
package http
