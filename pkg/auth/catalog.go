package auth

import (
	"fmt"
	"sync"
)

// Catalog is an in-memory store for principals, roles, and devices. Applications
// own persistence; Catalog is the v1 domain-first default.
type Catalog struct {
	mu         sync.RWMutex
	principals map[string]Principal
	roles      map[string]Role
	devices    map[string]DeviceIdentity
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		principals: make(map[string]Principal),
		roles:      make(map[string]Role),
		devices:    make(map[string]DeviceIdentity),
	}
}

// PutPrincipal upserts a principal.
func (c *Catalog) PutPrincipal(p Principal) error {
	if p.ID == "" {
		return fmt.Errorf("%w: principal id required", ErrInvalidConfig)
	}
	if p.Kind == "" {
		return fmt.Errorf("%w: principal kind required", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.principals[p.ID] = clonePrincipal(p)
	return nil
}

// GetPrincipal returns a principal by id.
func (c *Catalog) GetPrincipal(id string) (Principal, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.principals[id]
	if !ok {
		return Principal{}, fmt.Errorf("%w: %s", ErrPrincipalNotFound, id)
	}
	return clonePrincipal(p), nil
}

// PutRole upserts a role.
func (c *Catalog) PutRole(r Role) error {
	if r.Name == "" {
		return fmt.Errorf("%w: role name required", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roles[r.Name] = cloneRole(r)
	return nil
}

// GetRole returns a role by name.
func (c *Catalog) GetRole(name string) (Role, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.roles[name]
	if !ok {
		return Role{}, fmt.Errorf("%w: %s", ErrRoleNotFound, name)
	}
	return cloneRole(r), nil
}

// PutDevice upserts a device identity.
func (c *Catalog) PutDevice(d DeviceIdentity) error {
	if d.DeviceID == "" {
		return fmt.Errorf("%w: device id required", ErrInvalidConfig)
	}
	if d.TenantID == "" {
		return fmt.Errorf("%w: device tenant id required", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.devices[d.DeviceID] = cloneDevice(d)
	return nil
}

// GetDevice returns a device by id.
func (c *Catalog) GetDevice(id string) (DeviceIdentity, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.devices[id]
	if !ok {
		return DeviceIdentity{}, fmt.Errorf("%w: %s", ErrDeviceNotFound, id)
	}
	return cloneDevice(d), nil
}

// PermissionsFor returns the union of permissions for a principal in a tenant.
// RoleBindings on the tenant context take precedence when non-empty; otherwise
// roles are taken from the principal's TenantBindings for that tenant.
func (c *Catalog) PermissionsFor(p Principal, tenant TenantContext) ([]Permission, []string, error) {
	roles := append([]string(nil), tenant.RoleBindings...)
	if len(roles) == 0 {
		for _, b := range p.TenantBindings {
			if b.TenantID == tenant.TenantID || b.TenantID == "*" {
				roles = append(roles, b.Roles...)
			}
		}
	}
	roles = uniqueStrings(roles)

	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := make(map[Permission]struct{})
	out := make([]Permission, 0)
	for _, name := range roles {
		r, ok := c.roles[name]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", ErrRoleNotFound, name)
		}
		for _, perm := range r.Permissions {
			if _, ok := seen[perm]; ok {
				continue
			}
			seen[perm] = struct{}{}
			out = append(out, perm)
		}
	}
	return out, roles, nil
}

func clonePrincipal(p Principal) Principal {
	out := p
	if p.TenantBindings != nil {
		out.TenantBindings = append([]TenantBinding(nil), p.TenantBindings...)
		for i := range out.TenantBindings {
			out.TenantBindings[i].Roles = append([]string(nil), p.TenantBindings[i].Roles...)
		}
	}
	if p.Attributes != nil {
		out.Attributes = make(map[string]string, len(p.Attributes))
		for k, v := range p.Attributes {
			out.Attributes[k] = v
		}
	}
	return out
}

func cloneRole(r Role) Role {
	out := r
	if r.Permissions != nil {
		out.Permissions = append([]Permission(nil), r.Permissions...)
	}
	return out
}

func cloneDevice(d DeviceIdentity) DeviceIdentity {
	out := d
	if d.Metadata != nil {
		out.Metadata = make(map[string]string, len(d.Metadata))
		for k, v := range d.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
