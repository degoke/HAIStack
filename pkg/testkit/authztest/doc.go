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
// This package is for tests only. Production code must not import authztest.
package authztest
