// Package sdc provides FHIR R4 Structured Data Capture (SDC 3.0.0) behavior
// services without coupling them to HTTP, a frontend, or a database schema.
//
// # Canonical resource boundary
//
// FHIR resources enter and leave this package as *types.ResourceEnvelope
// values. JSON remains the stored source of truth, and callers can obtain the
// existing generated R4 representation with ParseR4 when typed protobuf access
// is needed. SDC does not define replacement FHIR resource or Bundle models.
//
// The package's questionnaire projection structs are compatibility views used
// internally by the behavior engine. Their JSON methods preserve FHIR
// polymorphic value keys and encode SDC behavior fields as extensions. New integrations should use the
// ResourceEnvelope APIs: ValidateQuestionnaireResource,
// ValidateQuestionnaireResponseResource, PopulateResource, ExtractResource,
// AssembleQuestionnaireResource, SaveQuestionnaireResource, and
// SaveQuestionnaireResponseResource.
//
// # Behavior services
//
// The package supports questionnaire normalization, linkId indexing, repeats,
// enableWhen rules, calculated expressions, population, response building,
// modular assembly,
// renderer-neutral form state, validation diagnostics, and transaction Bundle
// extraction. FHIRPath is supported through the existing pkg/fhirpath.Engine.
// CQL, FHIR Query, StructureMap, terminology, and adaptive behavior are
// explicit adapter contracts; unavailable adapters fail with diagnostics.
//
// Extraction returns a transaction Bundle as a *types.ResourceEnvelope. It does
// not persist or apply that Bundle. Callers may explicitly pass it to
// core.ResourceService.ProcessTransactionBundle or another transaction
// processor.
//
// HTTP and runtime integration lives in pkg/http and pkg/runtime. The HTTP
// adapter exposes SDC operations when an SDCService is configured, while the
// runtime supplies a default core/FHIRPath adapter and permits application
// policy injection through runtime.Builder.WithSDC.
package sdc
