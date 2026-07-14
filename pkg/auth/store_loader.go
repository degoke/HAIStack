package auth

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// NewEngineFromStore loads a tenant auth snapshot from persistent storage and
// builds an Engine from it.
func NewEngineFromStore(ctx context.Context, authStore store.AuthStore) (*Engine, error) {
	if authStore == nil {
		return nil, fmt.Errorf("%w: auth store required", ErrInvalidConfig)
	}
	snapshot, err := authStore.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := ConfigFromSnapshot(*snapshot)
	if err != nil {
		return nil, err
	}
	return NewEngine(cfg)
}

// ConfigFromSnapshot converts a persisted auth snapshot into an Engine Config.
func ConfigFromSnapshot(snapshot store.AuthSnapshot) (Config, error) {
	principals := make([]Principal, 0, len(snapshot.Principals))
	principalIndex := make(map[string]int, len(snapshot.Principals))
	for _, rec := range snapshot.Principals {
		p := Principal{
			ID:          rec.ID,
			Kind:        PrincipalKind(rec.Kind),
			DisplayName: rec.DisplayName,
			Attributes:  cloneStringMap(rec.Attributes),
		}
		principalIndex[p.ID] = len(principals)
		principals = append(principals, p)
	}
	for _, binding := range snapshot.Bindings {
		idx, ok := principalIndex[binding.PrincipalID]
		if !ok {
			continue
		}
		principals[idx].TenantBindings = append(principals[idx].TenantBindings, TenantBinding{
			TenantID: binding.TenantID,
			Roles:    append([]string(nil), binding.Roles...),
		})
	}

	roles := make([]Role, 0, len(snapshot.Roles))
	for _, rec := range snapshot.Roles {
		role := Role{Name: rec.Name}
		for _, permission := range rec.Permissions {
			role.Permissions = append(role.Permissions, Permission(permission))
		}
		roles = append(roles, role)
	}

	devices := make([]DeviceIdentity, 0, len(snapshot.Devices))
	for _, rec := range snapshot.Devices {
		devices = append(devices, DeviceIdentity{
			DeviceID:          rec.DeviceID,
			TenantID:          rec.TenantID,
			Status:            rec.Status,
			Trusted:           rec.Trusted,
			Metadata:          cloneStringMap(rec.Metadata),
			LinkedPrincipalID: rec.LinkedPrincipalID,
		})
	}

	if snapshot.Policy == nil {
		return Config{}, fmt.Errorf("%w: active policy required", ErrInvalidConfig)
	}

	format := PolicyFormat(snapshot.Policy.Format)
	if format == "" {
		format = PolicyFormatAuto
	}

	return Config{
		Principals:   principals,
		Roles:        roles,
		Devices:      devices,
		PolicyBytes:  append([]byte(nil), snapshot.Policy.Body...),
		PolicyFormat: format,
	}, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
