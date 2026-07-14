package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuthStore persists auth data for one tenant context in Postgres.
type AuthStore struct {
	exec     querier
	tenantID string
}

func newAuthStore(exec querier, tenantID string) *AuthStore {
	return &AuthStore{exec: exec, tenantID: tenantID}
}

func (s *AuthStore) UpsertPrincipal(ctx context.Context, principal store.AuthPrincipalRecord) error {
	attributes, err := json.Marshal(principal.Attributes)
	if err != nil {
		return fmt.Errorf("marshal principal attributes: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO auth_principal (id, kind, display_name, attributes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			kind = EXCLUDED.kind,
			display_name = EXCLUDED.display_name,
			attributes = EXCLUDED.attributes,
			updated_at = EXCLUDED.updated_at`,
		principal.ID, principal.Kind, nullString(principal.DisplayName), attributes,
		principal.CreatedAt, principal.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert principal: %w", err)
	}
	return nil
}

func (s *AuthStore) GetPrincipal(ctx context.Context, id string) (*store.AuthPrincipalRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT id, kind, COALESCE(display_name, ''), attributes, created_at, updated_at
		FROM auth_principal
		WHERE id = $1`, id)
	record, err := scanPostgresPrincipal(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("principal not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListPrincipals(ctx context.Context) ([]store.AuthPrincipalRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT id, kind, COALESCE(display_name, ''), attributes, created_at, updated_at
		FROM auth_principal
		ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list principals: %w", err)
	}
	defer rows.Close()
	var out []store.AuthPrincipalRecord
	for rows.Next() {
		record, err := scanPostgresPrincipal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan principal row: %w", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate principals: %w", err)
	}
	return out, nil
}

func (s *AuthStore) DeletePrincipal(ctx context.Context, id string) error {
	tag, err := s.exec.Exec(ctx, `DELETE FROM auth_principal WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete principal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("principal not found: %s", id)
	}
	return nil
}

func (s *AuthStore) UpsertRole(ctx context.Context, role store.AuthRoleRecord) error {
	permissions, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal role permissions: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO auth_role (name, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET
			permissions = EXCLUDED.permissions,
			updated_at = EXCLUDED.updated_at`,
		role.Name, permissions, role.CreatedAt, role.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert role: %w", err)
	}
	return nil
}

func (s *AuthStore) GetRole(ctx context.Context, name string) (*store.AuthRoleRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT name, permissions, created_at, updated_at
		FROM auth_role
		WHERE name = $1`, name)
	record, err := scanPostgresRole(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("role not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListRoles(ctx context.Context) ([]store.AuthRoleRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT name, permissions, created_at, updated_at
		FROM auth_role
		ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var out []store.AuthRoleRecord
	for rows.Next() {
		record, err := scanPostgresRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role row: %w", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	return out, nil
}

func (s *AuthStore) DeleteRole(ctx context.Context, name string) error {
	tag, err := s.exec.Exec(ctx, `DELETE FROM auth_role WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found: %s", name)
	}
	return nil
}

func (s *AuthStore) UpsertTenantBinding(ctx context.Context, binding store.AuthTenantBindingRecord) error {
	if err := s.ensureTenant(ctx); err != nil {
		return err
	}
	binding.TenantID = s.normalizeTenantID(binding.TenantID)
	roles, err := json.Marshal(binding.Roles)
	if err != nil {
		return fmt.Errorf("marshal binding roles: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO auth_tenant_binding (tenant_id, principal_id, roles, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, principal_id) DO UPDATE SET
			roles = EXCLUDED.roles,
			updated_at = EXCLUDED.updated_at`,
		binding.TenantID, binding.PrincipalID, roles, binding.CreatedAt, binding.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert tenant binding: %w", err)
	}
	return nil
}

func (s *AuthStore) GetTenantBinding(ctx context.Context, principalID string) (*store.AuthTenantBindingRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT tenant_id, principal_id, roles, created_at, updated_at
		FROM auth_tenant_binding
		WHERE tenant_id = $1 AND principal_id = $2`, s.tenantID, principalID)
	record, err := scanPostgresBinding(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("tenant binding not found: %s", principalID)
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant binding: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListTenantBindings(ctx context.Context) ([]store.AuthTenantBindingRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT tenant_id, principal_id, roles, created_at, updated_at
		FROM auth_tenant_binding
		WHERE tenant_id = $1
		ORDER BY principal_id ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant bindings: %w", err)
	}
	defer rows.Close()
	var out []store.AuthTenantBindingRecord
	for rows.Next() {
		record, err := scanPostgresBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan binding row: %w", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant bindings: %w", err)
	}
	return out, nil
}

func (s *AuthStore) DeleteTenantBinding(ctx context.Context, principalID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM auth_tenant_binding WHERE tenant_id = $1 AND principal_id = $2`,
		s.tenantID, principalID,
	)
	if err != nil {
		return fmt.Errorf("delete tenant binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant binding not found: %s", principalID)
	}
	return nil
}

func (s *AuthStore) UpsertDevice(ctx context.Context, device store.AuthDeviceRecord) error {
	if err := s.ensureTenant(ctx); err != nil {
		return err
	}
	device.TenantID = s.normalizeTenantID(device.TenantID)
	metadata, err := json.Marshal(device.Metadata)
	if err != nil {
		return fmt.Errorf("marshal device metadata: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO auth_device_identity (
			tenant_id, device_id, status, trusted, metadata, linked_principal_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, device_id) DO UPDATE SET
			status = EXCLUDED.status,
			trusted = EXCLUDED.trusted,
			metadata = EXCLUDED.metadata,
			linked_principal_id = EXCLUDED.linked_principal_id,
			updated_at = EXCLUDED.updated_at`,
		device.TenantID, device.DeviceID, device.Status, device.Trusted, metadata,
		nullString(device.LinkedPrincipalID), device.CreatedAt, device.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}
	return nil
}

func (s *AuthStore) GetDevice(ctx context.Context, deviceID string) (*store.AuthDeviceRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT tenant_id, device_id, status, trusted, metadata, COALESCE(linked_principal_id, ''), created_at, updated_at
		FROM auth_device_identity
		WHERE tenant_id = $1 AND device_id = $2`, s.tenantID, deviceID)
	record, err := scanPostgresDevice(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListDevices(ctx context.Context) ([]store.AuthDeviceRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT tenant_id, device_id, status, trusted, metadata, COALESCE(linked_principal_id, ''), created_at, updated_at
		FROM auth_device_identity
		WHERE tenant_id = $1
		ORDER BY device_id ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	var out []store.AuthDeviceRecord
	for rows.Next() {
		record, err := scanPostgresDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device row: %w", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}
	return out, nil
}

func (s *AuthStore) DeleteDevice(ctx context.Context, deviceID string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM auth_device_identity WHERE tenant_id = $1 AND device_id = $2`,
		s.tenantID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	return nil
}

func (s *AuthStore) UpsertPolicy(ctx context.Context, policy store.AuthPolicyRecord) error {
	if err := s.ensureTenant(ctx); err != nil {
		return err
	}
	policy.TenantID = s.normalizeTenantID(policy.TenantID)
	_, err := s.exec.Exec(ctx, `
		INSERT INTO auth_policy_document (
			tenant_id, name, format, version, body, active, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
			format = EXCLUDED.format,
			version = EXCLUDED.version,
			body = EXCLUDED.body,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at`,
		policy.TenantID, policy.Name, policy.Format, policy.Version, policy.Body, policy.Active,
		policy.CreatedAt, policy.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert policy: %w", err)
	}
	if policy.Active {
		_, err = s.exec.Exec(ctx, `
			UPDATE auth_policy_document
			SET active = FALSE
			WHERE tenant_id = $1 AND name <> $2 AND active = TRUE`,
			policy.TenantID, policy.Name,
		)
		if err != nil {
			return fmt.Errorf("deactivate other policies: %w", err)
		}
	}
	return nil
}

func (s *AuthStore) GetPolicy(ctx context.Context, name string) (*store.AuthPolicyRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = $1 AND name = $2`, s.tenantID, name)
	record, err := scanPostgresPolicy(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("policy not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}
	return record, nil
}

func (s *AuthStore) GetActivePolicy(ctx context.Context) (*store.AuthPolicyRecord, error) {
	row := s.exec.QueryRow(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = $1 AND active = TRUE
		ORDER BY updated_at DESC, name ASC
		LIMIT 1`, s.tenantID)
	record, err := scanPostgresPolicy(row)
	if isNoRows(err) {
		return nil, fmt.Errorf("active policy not found for tenant: %s", s.tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("get active policy: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListPolicies(ctx context.Context) ([]store.AuthPolicyRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = $1
		ORDER BY name ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()
	var out []store.AuthPolicyRecord
	for rows.Next() {
		record, err := scanPostgresPolicy(rows)
		if err != nil {
			return nil, fmt.Errorf("scan policy row: %w", err)
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policies: %w", err)
	}
	return out, nil
}

func (s *AuthStore) DeletePolicy(ctx context.Context, name string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM auth_policy_document WHERE tenant_id = $1 AND name = $2`,
		s.tenantID, name,
	)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy not found: %s", name)
	}
	return nil
}

func (s *AuthStore) Snapshot(ctx context.Context) (*store.AuthSnapshot, error) {
	principals, err := s.ListPrincipals(ctx)
	if err != nil {
		return nil, err
	}
	roles, err := s.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := s.ListTenantBindings(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := s.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := s.GetActivePolicy(ctx)
	if err != nil {
		return nil, err
	}
	return &store.AuthSnapshot{
		TenantID:   s.tenantID,
		Principals: principals,
		Roles:      roles,
		Bindings:   bindings,
		Devices:    devices,
		Policy:     policy,
	}, nil
}

func (s *AuthStore) ensureTenant(ctx context.Context) error {
	if s.tenantID == "" {
		return nil
	}
	_, err := s.exec.Exec(ctx, `
		INSERT INTO tenant (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, s.tenantID)
	if err != nil {
		return fmt.Errorf("ensure tenant %q: %w", s.tenantID, err)
	}
	return nil
}

func (s *AuthStore) normalizeTenantID(tenantID string) string {
	if tenantID != "" {
		return tenantID
	}
	return s.tenantID
}

type postgresScanner interface {
	Scan(dest ...any) error
}

func scanPostgresPrincipal(scanner postgresScanner) (*store.AuthPrincipalRecord, error) {
	var (
		record    store.AuthPrincipalRecord
		attrs     []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(&record.ID, &record.Kind, &record.DisplayName, &attrs, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &record.Attributes)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return &record, nil
}

func scanPostgresRole(scanner postgresScanner) (*store.AuthRoleRecord, error) {
	var (
		record    store.AuthRoleRecord
		payload   []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(&record.Name, &payload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &record.Permissions)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return &record, nil
}

func scanPostgresBinding(scanner postgresScanner) (*store.AuthTenantBindingRecord, error) {
	var (
		record    store.AuthTenantBindingRecord
		payload   []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(&record.TenantID, &record.PrincipalID, &payload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &record.Roles)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return &record, nil
}

func scanPostgresDevice(scanner postgresScanner) (*store.AuthDeviceRecord, error) {
	var (
		record    store.AuthDeviceRecord
		payload   []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(
		&record.TenantID, &record.DeviceID, &record.Status, &record.Trusted, &payload,
		&record.LinkedPrincipalID, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &record.Metadata)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return &record, nil
}

func scanPostgresPolicy(scanner postgresScanner) (*store.AuthPolicyRecord, error) {
	var (
		record    store.AuthPolicyRecord
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(
		&record.TenantID, &record.Name, &record.Format, &record.Version,
		&record.Body, &record.Active, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	return &record, nil
}
