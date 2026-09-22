# haistack-auth (`pkg/auth`)

Shared identity and policy library for HAIStack. It answers
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
- `CanBulkExport` — bulk export authorization (when wired)
- `CheckPatientScope` — patient-level compartment enforcement

Adapters wire into existing seams:

- `ViewAuthorizer` → `view.Authorizer`
- `AIPolicyAdapter` → `ai.PolicyEngine`
- `AuditingEngine` → optional emit of allow/deny through `pkg/audit` (auth does not own audit storage)

## What it does not do

- Issue or validate OAuth2/OIDC tokens (see `pkg/oauth`, `pkg/smart`)
- Store users in SQL — supply `Catalog` data from the host application
- Enforce consent, break-glass, or security labels
- Replace SMART scope parsing — intersect scopes in middleware, then call the engine

## How it fits in the ecosystem

```text
  HTTP / SMART middleware          pkg/view / pkg/ai / pkg/modules / pkg/sync
              |                              |
              v                              v
        resolve Principal + TenantContext
              |
              v
         auth.Engine (policy DSL + Catalog)
              |
              +---- ViewAuthorizer ----> view.Executor
              +---- AIPolicyAdapter ---> ai.Executor
              +---- direct APIs ------> CanInstallModule, CanPushDeviceEvent, ...
              |
              v (optional)
         AuditingEngine ----> pkg/audit.LogAuthDecision
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Peer | **smart** | Scopes/tokens parsed upstream; engine evaluates tenant policy |
| Peer | **view** | `ViewAuthorizer` implements `view.Authorizer` |
| Peer | **ai** | `AIPolicyAdapter` implements `ai.PolicyEngine` + optional `AIConstraints` |
| Peer | **audit** | Decision emit only; no storage ownership |
| Peer | **modules** | `CanInstallModule` before install/upgrade |
| Peer | **sync** | `CanPushDeviceEvent` for trusted device checks |
| Peer | **export** | `CanBulkExport` when bulk jobs require explicit allow rules |
| Downstream | **testkit/authztest** | Scenario catalog for CI matrices (not production import) |

## When to use it

- Centralizing allow/deny for FHIR reads/writes beyond coarse SMART scopes
- Wiring view execution permissions to roles and named views
- Gating AI tools with the same principals used for human users
- Validating sync device identity before accepting push batches
- Authorizing module installs per tenant and module name
- Enforcing patient compartment rules for portal or scoped agents

## Usage modes

### 1. Standalone engine (in-memory catalog)

Load roles, principals, devices, and policy bytes at process startup — typical for edge nodes and tests:

```go
eng, err := auth.NewEngine(auth.Config{
    Roles:       []auth.Role{/* ... */},
    Principals:  []auth.Principal{/* ... */},
    Devices:     []auth.DeviceIdentity{/* ... */},
    PolicyBytes: policyJSON,
})
```

### 2. View layer adapter

Map HTTP/session actors to principals once, then reuse `ViewAuthorizer` on every `Execute`:

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

### 3. AI policy adapter (engine + field constraints)

Use `AIPolicyAdapter` when auth rules should drive tool allow-lists; narrow read/search/write/view shapes with `AIConstraints`:

```go
policy := &auth.AIPolicyAdapter{
    Engine:      eng,
    TenantID:    "tenant-a",
    Resolve:     resolveActor,
    Constraints: &auth.AIConstraints{ /* Search/Write maps */ },
}
exec, err := ai.NewExecutor(ai.Config{Policy: policy, /* ... */ })
```

### 4. Audited decisions

Wrap the engine to append audit rows without changing call sites:

```go
audited := &auth.AuditingEngine{
    Engine: eng,
    Log:    auditLogger, // audit.Logger
}
d, err := audited.CanReadResource(ctx, req)
```

### 5. Device and sync authorization

Register devices in `Catalog` and match policy rules on `push-device-event`:

```go
d, err := eng.CanPushDeviceEvent(ctx, auth.DevicePushRequest{
    Principal: devicePrincipal,
    Tenant:    auth.TenantContext{TenantID: "tenant-a"},
    DeviceID:  "device-1",
})
```

### 6. YAML/JSON policy reload (application-owned)

Parse and compile policy separately when the host app hot-reloads rules:

```go
compiled, err := auth.ParseAndCompilePolicy(policyBytes, auth.PolicyFormatJSON)
eng, err := auth.NewEngine(auth.Config{Compiled: compiled, /* catalog */ })
```

## Examples

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

**Execute view and AI tool checks:**

```go
viewDecision, err := eng.CanExecuteView(ctx, auth.ViewRequest{
    Principal: principal,
    Tenant:    auth.TenantContext{TenantID: "tenant-a"},
    ViewName:  "patient_summary_view",
})
toolDecision, err := eng.CanExecuteAITool(ctx, auth.AIToolRequest{
    Principal: principal,
    Tenant:    auth.TenantContext{TenantID: "tenant-a"},
    ToolName:  "read_fhir_resource",
})
```

**Patient compartment:**

```go
scoped := auth.TenantContext{
    TenantID:     "tenant-a",
    PatientScope: "pat-1",
}
d, err := eng.CheckPatientScope(ctx, auth.PatientScopeRequest{
    Principal: portalUser,
    Tenant:    scoped,
    PatientID: "pat-1",
})
// Reads outside the compartment are denied via CanReadResource + scoped TenantContext
```

**Module install:**

```go
d, err := eng.CanInstallModule(ctx, auth.ModuleInstallRequest{
    Principal:  installer,
    Tenant:     auth.TenantContext{TenantID: "tenant-a"},
    ModuleName: "scheduling",
})
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
- Patient compartment: scoped principals may only access their patient id and linked resources
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

## Testing

```bash
go test ./pkg/auth/... -count=1
```

For cross-package authorization matrices, use `pkg/testkit/authztest` (`RunAll`, YAML catalogues) — production code must not import testkit.

## MVP limits

- No OAuth2/OIDC or SMART scopes (see `pkg/smart`)
- No consent engine or security labels
- No break-glass workflow
- No full ABAC attribute language
- No DB user tables — supply principals/roles/devices from the host app

See [doc.go](./doc.go) for the full API and package boundaries.
