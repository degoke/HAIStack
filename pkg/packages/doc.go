// Package packages installs FHIR NPM packages from packages.fhir.org (or local
// directories) into the shared registry catalog managed by pkg/registry.
//
// Use pkg/modules for first-party module.json capability bundles in the repo;
// use this package for upstream HL7 and vendor IGs published as NPM packages.
//
// See README.md in this directory for InstallConfigured, registry installs,
// and runtime startup wiring.
package packages
