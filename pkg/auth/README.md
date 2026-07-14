# haistack-auth (`pkg/auth`)

Shared identity and policy library for Health AI Stack. It answers
authorization questions for the rest of the stack — not HTTP auth flows,
database user tables, or SMART launch logic.

## What it does

v1 centers on principals, roles, permissions, tenant context, device trust,
and a deny-by-default policy DSL:

- `CanReadResource` / `CanWriteResource` — FHIR resource access
- `CanExecuteView` — view execution with declared permissions
- `CanExecuteAITool` — AI tool execution
- `CanPushDeviceEvent` — sync device trust for a tenant
- `CanInstallModule` — module install authorization
- `CheckPatientScope` — patient-level access stub

Adapters wire into existing seams:

- `ViewAuthorizer` → `view.Authorizer`
- `AIPolicyAdapter` → `ai.PolicyEngine`
- `AuditingEngine` → optional emit of allow/deny through `pkg/audit` (auth does not own audit storage)

## Usage

**Build an engine:**

```go
eng, err := auth.NewEngine(auth.Config{
    Roles: []auth.Role{{
        Name: "clinician",
        Permissions: []auth.Permission{
            "appointment.read",
            "read-patient-summary",
            "module.install",
        },
    }},
    Principals: []auth.Principal{{
        ID:   "user-1",
        Kind: auth.KindUser,
        TenantBindings: []auth.TenantBinding{{
            TenantID: "tenant-a",
            Roles:    []string{"clinician"},
        }},
    }},
    Devices: []auth.DeviceIdentity{{
        DeviceID: "device-1",
        TenantID: "tenant-a",
        Status:   auth.DeviceStatusActive,
        Trusted:  true,
    }},
    PolicyBytes: []byte(`{
      "version": "1",
      "rules": [
        {
          "name": "appointment-rw",
          "effect": "allow",
          "match": {
            "actions": ["read", "write"],
            "resourceTypes": ["Appointment"],
            "anyPermissions": ["appointment.read"]
          },
          "reason": "clinicians may access appointments"
        },
        {
          "name": "device-push",
          "effect": "allow",
          "match": {
            "actions": ["push-device-event"],
            "deviceTrusted": true,
            "deviceStatuses": ["active"]
          }
        },
        {
          "name": "install-scheduling",
          "effect": "allow",
          "match": {
            "actions": ["install-module"],
            "moduleNames": ["scheduling"],
            "roles": ["clinician"]
          }
        }
      ]
    }`),
})
```

**Authorize a read:**

```go
d, err := eng.CanReadResource(ctx, auth.ReadRequest{
    Principal:    principal,
    Tenant:       auth.TenantContext{TenantID: "tenant-a"},
    ResourceType: "Appointment",
    ID:           "appt-1",
})
// d.Allowed, d.Reason
```

**Wire into view:**

```go
authorizer := &auth.ViewAuthorizer{
    Engine:   eng,
    TenantID: "tenant-a",
    Resolve: func(ctx context.Context, actor, subject string) (auth.Principal, auth.TenantContext, error) {
        p, err := eng.Catalog().GetPrincipal(actor)
        return p, auth.TenantContext{TenantID: "tenant-a"}, err
    },
}
```

**Wire into AI:**

```go
policy := &auth.AIPolicyAdapter{
    Engine:   eng,
    TenantID: "tenant-a",
    Resolve:  resolveActor,
    Constraints: &auth.AIConstraints{
        Search: map[string]ai.SearchTypePolicy{
            "Patient": {AllowedParams: []string{"name", "telecom"}},
        },
        Write: map[string]ai.WriteTypePolicy{
            "Patient": {CreateFields: []string{"name"}, UpdateFields: []string{"name"}},
        },
    },
}
```

## Policy DSL

Rules are loaded from JSON or YAML, compiled in memory, and evaluated in order.
The first matching rule wins. When no rule matches, access is **denied**.

Match fields (empty = any):

| Field | Meaning |
|-------|---------|
| `principalKinds` | `user`, `device`, `service`, `ai-agent` |
| `tenants` | tenant ids (`*` wildcard) |
| `roles` | any overlapping role |
| `anyPermissions` / `allPermissions` | permission checks |
| `resourceTypes` | FHIR resource type |
| `actions` | `read`, `write`, `execute-view`, … |
| `viewNames` / `toolNames` / `moduleNames` | named targets |
| `purposeOfUse` | simple attribute match |
| `deviceTrusted` / `deviceStatuses` | device trust |
| `patientScoped` | whether tenant patient scope is set |

Permissions treat `appointment.read` and `read-appointment` as equivalent.

## Safety model

- Deny by default
- Explicit allow rules
- Tenant binding required for user principals
- Device push requires registered, trusted, active device for the target tenant
- Patient scope stub: scoped principals may only access their patient id
- Persistence is application-owned; Catalog is in-memory by default

## Where it fits

| Layer | Role |
|-------|------|
| **auth** | Identity and policy decisions (this package) |
| **audit** | Shared audit event library; auth may emit via `AuditingEngine` |
| **view** | Consumes `ViewAuthorizer` |
| **ai** | Consumes `AIPolicyAdapter` |
| **modules** | Asks `CanInstallModule` before install |
| **sync** | Asks `CanPushDeviceEvent` for device trust |
| **smart** | Optional SMART scopes/tokens/launch via `pkg/smart` adapters (out of scope here) |

## MVP limits

- No OAuth2/OIDC or SMART scopes (see `pkg/smart`)
- No consent engine or security labels
- No break-glass workflow
- No full ABAC attribute language
- No DB user tables — supply principals/roles/devices from the host app

See [doc.go](./doc.go) for the full API and package boundaries.
