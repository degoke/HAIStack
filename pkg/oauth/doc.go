// Package oauth provides a built-in OAuth2/OIDC authorization server for SMART on FHIR.
//
// Hosts can mount oauth.Server alongside pkg/http FHIR handlers to issue and validate
// access tokens without an external authorization server. The server exposes:
//
//   - /.well-known/openid-configuration
//   - /.well-known/smart-configuration
//   - /oauth/authorize
//   - /oauth/token
//   - /oauth/jwks
//   - /oauth/register
//
// Durable state is provided by pkg/oauth/store (Postgres or SQLite JSON payload tables)
// or file-backed helpers in production.go for single-node dev. haistack serve wires
// builtin OAuth via runtime.WithBuiltinOAuth when oauth.enabled is true.
package oauth
