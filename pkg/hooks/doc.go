// Package hooks is a small FHIR intercept SPI for HAIStack.
//
// HAPI's interceptor bus has dozens of pointcuts. This package keeps four:
//
//   - Incoming    — HTTP request after routing, before the handler runs
//   - PreStorage  — core write path, after validation/id assignment, before persist
//   - PostCommit  — core write path, after the write session commits
//   - Outgoing    — HTTP response, before a resource envelope is serialized
//
// Register Func values on a Registry with On, then wire the same Registry into
// core.ResourceServiceConfig.Hooks and http.Config.Hooks (runtime.Builder.WithHooks
// does both). Hooks run in registration order. Incoming, pre-storage, and outgoing
// errors abort the request; post-commit errors are ignored so a successful write
// is not reported as failure.
//
// Do not add more pointcuts here. Extend by registering another Func on one of
// these four, or by wrapping HTTP middleware for transport concerns.
package hooks
