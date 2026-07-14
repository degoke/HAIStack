// Package auth implements haistack-auth, the shared identity and policy library
// for Health AI Stack.
//
// # Scope
//
// v1 answers authorization questions for the rest of the stack. It does not own
// HTTP auth flows, database user tables, OAuth2/OIDC, or SMART launch logic.
//
// Contained in v1:
//
//   - Principal, Role, Permission
//   - TenantContext
//   - DeviceIdentity and trust state
//   - PolicyEngine / Engine decision APIs
//   - Patient-level access stub
//   - Module install checks
//   - Core policy DSL (JSON/YAML)
//   - Adapters for view.Authorizer and ai.PolicyEngine
//   - Optional AuditingEngine wrapper that emits decisions via pkg/audit
//
// Out of v1: OAuth2/OIDC, SMART scopes, consent enforcement, security labels,
// break-glass workflows, full ABAC, and DB-specific user schemas.
// Auth does not own audit storage; use pkg/audit (and AuditingEngine) to emit.
//
// # Public API
//
//   - Engine / NewEngine: compiles policy, holds an in-memory Catalog, and
//     answers CanReadResource, CanWriteResource, CanExecuteView,
//     CanExecuteAITool, CanPushDeviceEvent, CanInstallModule, and
//     CheckPatientScope.
//   - Catalog: application-supplied principals, roles, and devices.
//   - PolicyDocument / ParsePolicy / CompilePolicy: deny-by-default DSL with
//     ordered allow/deny rules.
//   - ViewAuthorizer: adapts Engine to view.Authorizer.
//   - AIPolicyAdapter: adapts Engine to ai.PolicyEngine; optional AIConstraints
//     keep field/param/de-identify/approval narrowing in the AI layer.
//   - AuditingEngine: wraps PolicyEngine and emits allow/deny via pkg/audit.
//
// # Typical usage
//
//	eng, err := auth.NewEngine(auth.Config{
//	    Roles: []auth.Role{{
//	        Name: "clinician",
//	        Permissions: []auth.Permission{"appointment.read", "read-patient-summary"},
//	    }},
//	    Principals: []auth.Principal{{
//	        ID:   "user-1",
//	        Kind: auth.KindUser,
//	        TenantBindings: []auth.TenantBinding{{
//	            TenantID: "tenant-a",
//	            Roles:    []string{"clinician"},
//	        }},
//	    }},
//	    Devices: []auth.DeviceIdentity{{
//	        DeviceID: "device-1",
//	        TenantID: "tenant-a",
//	        Status:   auth.DeviceStatusActive,
//	        Trusted:  true,
//	    }},
//	    PolicyBytes: policyJSON,
//	})
//
//	d, err := eng.CanReadResource(ctx, auth.ReadRequest{
//	    Principal:    principal,
//	    Tenant:       auth.TenantContext{TenantID: "tenant-a"},
//	    ResourceType: "Appointment",
//	    ID:           "appt-1",
//	})
//
// # Execution model
//
// Decision methods resolve role permissions from Catalog, apply tenant binding
// and patient-scope stub checks, then evaluate CompiledPolicy rules in order.
// The first matching rule wins. When no rule matches, access is denied.
//
// # Integration points
//
//   - haistack-view: ViewAuthorizer implements view.Authorizer.
//   - haistack-ai: AIPolicyAdapter implements ai.PolicyEngine.
//   - haistack-modules: CanInstallModule checks installer permissions and module
//     policy before install (execution stays in pkg/modules).
//   - haistack-sync: CanPushDeviceEvent evaluates device trust for a tenant
//     (transport stays in pkg/sync).
//   - haistack-audit: AuditingEngine emits decision events; storage stays in
//     store.AuditStore adapters (postgres/sqlite).
//   - haistack-postgres: tenant scoping stays in adapters; auth only decides
//     whether a principal may act in a tenant.
package auth
