// Package authztest provides a reusable authorization scenario test kit for
// Health AI Stack. It exercises pkg/auth, pkg/smart, pkg/view, pkg/ai, and
// related seams with documented principal/policy fixtures and table-driven
// allow/deny expectations.
//
// OAuth token exchange success is not authorization. Scenarios assert semantic
// access-control outcomes: restricted principals, patient-compartment
// boundaries, policy narrowing of SMART scopes, token expiry, and per-path
// denials across REST, search, views, AI tools, sync, and module install.
//
// Machine-readable YAML catalogues (research/policy-semantics and testdata)
// are loaded with ParseYAML / LoadYAMLFile and executed through
// ScenariosFromYAML. YAML cases evaluate SMART scope grants ∩ pkg/auth policy
// allows, matching the policy-semantics research artefact. Principals, roles,
// and policy documents are declared in the YAML and drive the SMART adapter.
// Tenant and kind are required (no TenantA or KindUser fallback).
// policyRoleGrants / roleGrants overlay extra role permissions per named
// policy or scenario. YAML catalogues run with RunYAML and do not construct
// Go BaseConfig kits. Scenario principal, scopes, action, and policy are
// required (no clinician / user/*.read / read / base fallbacks).
//
// This package is for tests and the Track C research CLI
// (research/policy-semantics), which issue #11 places in authztest. Other
// production packages must not import authztest.
package authztest
