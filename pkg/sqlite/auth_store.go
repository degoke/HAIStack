package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuthStore persists auth data for one logical tenant in SQLite.
type AuthStore struct {
	exec     moduleExec
	tenantID string
}

func newAuthStore(db *sql.DB, tenantID string) *AuthStore {
	return &AuthStore{exec: db, tenantID: tenantID}
}

func (s *AuthStore) UpsertPrincipal(ctx context.Context, principal store.AuthPrincipalRecord) error {
	attributes, err := json.Marshal(principal.Attributes)
	if err != nil {
		return fmt.Errorf("marshal principal attributes: %w", err)
	}
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO auth_principal (id, kind, display_name, attributes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind = excluded.kind,
			display_name = excluded.display_name,
			attributes = excluded.attributes,
			updated_at = excluded.updated_at`,
		principal.ID, principal.Kind, principal.DisplayName, attributes,
		formatTime(principal.CreatedAt), formatTime(principal.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert principal: %w", err)
	}
	return nil
}

func (s *AuthStore) GetPrincipal(ctx context.Context, id string) (*store.AuthPrincipalRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT id, kind, display_name, attributes, created_at, updated_at
		FROM auth_principal WHERE id = ?`, id)
	record, err := scanSQLitePrincipal(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("principal not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListPrincipals(ctx context.Context) ([]store.AuthPrincipalRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT id, kind, display_name, attributes, created_at, updated_at
		FROM auth_principal
		ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list principals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuthPrincipalRecord
	for rows.Next() {
		record, err := scanSQLitePrincipal(rows)
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
	result, err := s.exec.ExecContext(ctx, `DELETE FROM auth_principal WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete principal: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete principal rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("principal not found: %s", id)
	}
	return nil
}

func (s *AuthStore) UpsertRole(ctx context.Context, role store.AuthRoleRecord) error {
	permissions, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal role permissions: %w", err)
	}
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO auth_role (name, permissions, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			permissions = excluded.permissions,
			updated_at = excluded.updated_at`,
		role.Name, permissions, formatTime(role.CreatedAt), formatTime(role.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert role: %w", err)
	}
	return nil
}

func (s *AuthStore) GetRole(ctx context.Context, name string) (*store.AuthRoleRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT name, permissions, created_at, updated_at
		FROM auth_role WHERE name = ?`, name)
	record, err := scanSQLiteRole(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("role not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListRoles(ctx context.Context) ([]store.AuthRoleRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT name, permissions, created_at, updated_at
		FROM auth_role
		ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuthRoleRecord
	for rows.Next() {
		record, err := scanSQLiteRole(rows)
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
	result, err := s.exec.ExecContext(ctx, `DELETE FROM auth_role WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete role rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("role not found: %s", name)
	}
	return nil
}

func (s *AuthStore) UpsertTenantBinding(ctx context.Context, binding store.AuthTenantBindingRecord) error {
	binding.TenantID = s.normalizeTenantID(binding.TenantID)
	roles, err := json.Marshal(binding.Roles)
	if err != nil {
		return fmt.Errorf("marshal binding roles: %w", err)
	}
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO auth_tenant_binding (tenant_id, principal_id, roles, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, principal_id) DO UPDATE SET
			roles = excluded.roles,
			updated_at = excluded.updated_at`,
		binding.TenantID, binding.PrincipalID, roles,
		formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert tenant binding: %w", err)
	}
	return nil
}

func (s *AuthStore) GetTenantBinding(ctx context.Context, principalID string) (*store.AuthTenantBindingRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT tenant_id, principal_id, roles, created_at, updated_at
		FROM auth_tenant_binding
		WHERE tenant_id = ? AND principal_id = ?`, s.tenantID, principalID)
	record, err := scanSQLiteBinding(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tenant binding not found: %s", principalID)
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant binding: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListTenantBindings(ctx context.Context) ([]store.AuthTenantBindingRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT tenant_id, principal_id, roles, created_at, updated_at
		FROM auth_tenant_binding
		WHERE tenant_id = ?
		ORDER BY principal_id ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuthTenantBindingRecord
	for rows.Next() {
		record, err := scanSQLiteBinding(rows)
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
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM auth_tenant_binding WHERE tenant_id = ? AND principal_id = ?`,
		s.tenantID, principalID,
	)
	if err != nil {
		return fmt.Errorf("delete tenant binding: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete tenant binding rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tenant binding not found: %s", principalID)
	}
	return nil
}

func (s *AuthStore) UpsertDevice(ctx context.Context, device store.AuthDeviceRecord) error {
	device.TenantID = s.normalizeTenantID(device.TenantID)
	metadata, err := json.Marshal(device.Metadata)
	if err != nil {
		return fmt.Errorf("marshal device metadata: %w", err)
	}
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO auth_device_identity (
			tenant_id, device_id, status, trusted, metadata, linked_principal_id, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, device_id) DO UPDATE SET
			status = excluded.status,
			trusted = excluded.trusted,
			metadata = excluded.metadata,
			linked_principal_id = excluded.linked_principal_id,
			updated_at = excluded.updated_at`,
		device.TenantID, device.DeviceID, device.Status, boolToInt(device.Trusted), metadata,
		nullSQLiteString(device.LinkedPrincipalID), formatTime(device.CreatedAt), formatTime(device.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}
	return nil
}

func (s *AuthStore) GetDevice(ctx context.Context, deviceID string) (*store.AuthDeviceRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT tenant_id, device_id, status, trusted, metadata, linked_principal_id, created_at, updated_at
		FROM auth_device_identity
		WHERE tenant_id = ? AND device_id = ?`, s.tenantID, deviceID)
	record, err := scanSQLiteDevice(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListDevices(ctx context.Context) ([]store.AuthDeviceRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT tenant_id, device_id, status, trusted, metadata, linked_principal_id, created_at, updated_at
		FROM auth_device_identity
		WHERE tenant_id = ?
		ORDER BY device_id ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuthDeviceRecord
	for rows.Next() {
		record, err := scanSQLiteDevice(rows)
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
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM auth_device_identity WHERE tenant_id = ? AND device_id = ?`,
		s.tenantID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete device rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	return nil
}

func (s *AuthStore) UpsertPolicy(ctx context.Context, policy store.AuthPolicyRecord) error {
	policy.TenantID = s.normalizeTenantID(policy.TenantID)
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO auth_policy_document (
			tenant_id, name, format, version, body, active, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, name) DO UPDATE SET
			format = excluded.format,
			version = excluded.version,
			body = excluded.body,
			active = excluded.active,
			updated_at = excluded.updated_at`,
		policy.TenantID, policy.Name, policy.Format, policy.Version, policy.Body,
		boolToInt(policy.Active), formatTime(policy.CreatedAt), formatTime(policy.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert policy: %w", err)
	}
	if policy.Active {
		_, err = s.exec.ExecContext(ctx, `
			UPDATE auth_policy_document
			SET active = 0
			WHERE tenant_id = ? AND name <> ? AND active <> 0`,
			policy.TenantID, policy.Name,
		)
		if err != nil {
			return fmt.Errorf("deactivate other policies: %w", err)
		}
	}
	return nil
}

func (s *AuthStore) GetPolicy(ctx context.Context, name string) (*store.AuthPolicyRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = ? AND name = ?`, s.tenantID, name)
	record, err := scanSQLitePolicy(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("policy not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}
	return record, nil
}

func (s *AuthStore) GetActivePolicy(ctx context.Context) (*store.AuthPolicyRecord, error) {
	row := s.exec.QueryRowContext(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = ? AND active <> 0
		ORDER BY updated_at DESC, name ASC
		LIMIT 1`, s.tenantID)
	record, err := scanSQLitePolicy(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active policy not found for tenant: %s", s.tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("get active policy: %w", err)
	}
	return record, nil
}

func (s *AuthStore) ListPolicies(ctx context.Context) ([]store.AuthPolicyRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT tenant_id, name, format, version, body, active, created_at, updated_at
		FROM auth_policy_document
		WHERE tenant_id = ?
		ORDER BY name ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuthPolicyRecord
	for rows.Next() {
		record, err := scanSQLitePolicy(rows)
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
	result, err := s.exec.ExecContext(ctx, `
		DELETE FROM auth_policy_document WHERE tenant_id = ? AND name = ?`,
		s.tenantID, name,
	)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete policy rows affected: %w", err)
	}
	if rows == 0 {
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

func (s *AuthStore) normalizeTenantID(tenantID string) string {
	if tenantID != "" {
		return tenantID
	}
	return s.tenantID
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullSQLiteString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type sqliteScanner interface {
	Scan(dest ...any) error
}

func scanSQLitePrincipal(scanner sqliteScanner) (*store.AuthPrincipalRecord, error) {
	var (
		record            store.AuthPrincipalRecord
		attributesPayload []byte
		createdAt         string
		updatedAt         string
	)
	if err := scanner.Scan(&record.ID, &record.Kind, &record.DisplayName, &attributesPayload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(attributesPayload) > 0 {
		_ = json.Unmarshal(attributesPayload, &record.Attributes)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanSQLiteRole(scanner sqliteScanner) (*store.AuthRoleRecord, error) {
	var (
		record             store.AuthRoleRecord
		permissionsPayload []byte
		createdAt          string
		updatedAt          string
	)
	if err := scanner.Scan(&record.Name, &permissionsPayload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(permissionsPayload) > 0 {
		_ = json.Unmarshal(permissionsPayload, &record.Permissions)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanSQLiteBinding(scanner sqliteScanner) (*store.AuthTenantBindingRecord, error) {
	var (
		record       store.AuthTenantBindingRecord
		rolesPayload []byte
		createdAt    string
		updatedAt    string
	)
	if err := scanner.Scan(&record.TenantID, &record.PrincipalID, &rolesPayload, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if len(rolesPayload) > 0 {
		_ = json.Unmarshal(rolesPayload, &record.Roles)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanSQLiteDevice(scanner sqliteScanner) (*store.AuthDeviceRecord, error) {
	var (
		record          store.AuthDeviceRecord
		trusted         int
		metadataPayload []byte
		linkedPrincipal sql.NullString
		createdAt       string
		updatedAt       string
	)
	if err := scanner.Scan(
		&record.TenantID, &record.DeviceID, &record.Status, &trusted, &metadataPayload,
		&linkedPrincipal, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	record.Trusted = trusted != 0
	record.LinkedPrincipalID = linkedPrincipal.String
	if len(metadataPayload) > 0 {
		_ = json.Unmarshal(metadataPayload, &record.Metadata)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanSQLitePolicy(scanner sqliteScanner) (*store.AuthPolicyRecord, error) {
	var (
		record    store.AuthPolicyRecord
		active    int
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(
		&record.TenantID, &record.Name, &record.Format, &record.Version,
		&record.Body, &active, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	record.Active = active != 0
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
