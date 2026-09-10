// Package oauth provides an embeddable OAuth 2.0 / SMART-on-FHIR authorization
// server for self-contained HAIStack deployments.
//
// Hosts that already use Keycloak, Auth0, or an EHR IdP can omit this package
// and continue wiring external token validation through pkg/smart and pkg/http.
//
// Features:
//   - Static client registry (authorization code + PKCE, optional client credentials)
//   - Refresh tokens with rotation (offline_access scope)
//   - Token revocation endpoint (RFC 7009-style)
//   - JWT access tokens and OIDC id_tokens (RS256 or HS256)
//   - SMART /.well-known/smart-configuration and OIDC /.well-known/openid-configuration
//   - WireHTTP for PrincipalResolver integration with pkg/http
//   - Optional SQLite persistence via oauth/store
//
// Authorization decisions remain in pkg/auth; scope parsing and JWT validation
// reuse pkg/smart.
package oauth
