package store

import (
	"context"
	"time"
)

// AuthPrincipalRecord stores one principal identity used by pkg/auth.
type AuthPrincipalRecord struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	DisplayName string            `json:"displayName,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// AuthRoleRecord stores one named role and its permissions.
type AuthRoleRecord struct {
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AuthTenantBindingRecord links a principal to roles in one tenant.
type AuthTenantBindingRecord struct {
	TenantID    string    `json:"tenantId"`
	PrincipalID string    `json:"principalId"`
	Roles       []string  `json:"roles,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AuthDeviceRecord stores tenant-scoped device trust state.
type AuthDeviceRecord struct {
	DeviceID          string            `json:"deviceId"`
	TenantID          string            `json:"tenantId"`
	Status            string            `json:"status"`
	Trusted           bool              `json:"trusted"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	LinkedPrincipalID string            `json:"linkedPrincipalId,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

// AuthPolicyRecord stores one persisted policy document.
type AuthPolicyRecord struct {
	TenantID  string    `json:"tenantId"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Version   string    `json:"version"`
	Body      []byte    `json:"body"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AuthSnapshot is the tenant-scoped auth dataset needed to hydrate pkg/auth.
type AuthSnapshot struct {
	TenantID   string                    `json:"tenantId"`
	Principals []AuthPrincipalRecord     `json:"principals,omitempty"`
	Roles      []AuthRoleRecord          `json:"roles,omitempty"`
	Bindings   []AuthTenantBindingRecord `json:"bindings,omitempty"`
	Devices    []AuthDeviceRecord        `json:"devices,omitempty"`
	Policy     *AuthPolicyRecord         `json:"policy,omitempty"`
}

// AuthStore persists and loads auth identities, roles, bindings, devices, and
// policy documents for one tenant context.
type AuthStore interface {
	UpsertPrincipal(ctx context.Context, principal AuthPrincipalRecord) error
	GetPrincipal(ctx context.Context, id string) (*AuthPrincipalRecord, error)
	ListPrincipals(ctx context.Context) ([]AuthPrincipalRecord, error)
	DeletePrincipal(ctx context.Context, id string) error

	UpsertRole(ctx context.Context, role AuthRoleRecord) error
	GetRole(ctx context.Context, name string) (*AuthRoleRecord, error)
	ListRoles(ctx context.Context) ([]AuthRoleRecord, error)
	DeleteRole(ctx context.Context, name string) error

	UpsertTenantBinding(ctx context.Context, binding AuthTenantBindingRecord) error
	GetTenantBinding(ctx context.Context, principalID string) (*AuthTenantBindingRecord, error)
	ListTenantBindings(ctx context.Context) ([]AuthTenantBindingRecord, error)
	DeleteTenantBinding(ctx context.Context, principalID string) error

	UpsertDevice(ctx context.Context, device AuthDeviceRecord) error
	GetDevice(ctx context.Context, deviceID string) (*AuthDeviceRecord, error)
	ListDevices(ctx context.Context) ([]AuthDeviceRecord, error)
	DeleteDevice(ctx context.Context, deviceID string) error

	UpsertPolicy(ctx context.Context, policy AuthPolicyRecord) error
	GetPolicy(ctx context.Context, name string) (*AuthPolicyRecord, error)
	GetActivePolicy(ctx context.Context) (*AuthPolicyRecord, error)
	ListPolicies(ctx context.Context) ([]AuthPolicyRecord, error)
	DeletePolicy(ctx context.Context, name string) error

	Snapshot(ctx context.Context) (*AuthSnapshot, error)
}
