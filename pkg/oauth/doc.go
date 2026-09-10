// Package oauth provides an embeddable OAuth 2.0 / SMART-on-FHIR authorization
// server for self-contained HAIStack deployments.
//
// Hosts that already use Keycloak, Auth0, or an EHR IdP can omit this package
// and continue wiring external token validation through pkg/smart and pkg/http.
//
// v1 scope:
//   - Static client registry (authorization code + PKCE, optional client credentials)
//   - JWT access tokens (RS256 or HS256)
//   - SMART /.well-known/smart-configuration
//   - WireHTTP for PrincipalResolver integration with pkg/http
//
// Authorization decisions remain in pkg/auth; scope parsing and JWT validation
// reuse pkg/smart.
package oauth
