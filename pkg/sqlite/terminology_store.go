package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type TerminologyStore struct {
	exec  queryExec
	scope string
}

func newTerminologyStore(e queryExec, scope string) *TerminologyStore {
	if scope == "" {
		scope = "default"
	}
	return &TerminologyStore{exec: e, scope: scope}
}
func (s *TerminologyStore) FindResource(ctx context.Context, scope, typ, url, ver string) (*store.TerminologyResourceRecord, error) {
	var r store.TerminologyResourceRecord
	err := s.exec.QueryRowContext(ctx, `SELECT scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module FROM terminology_resource WHERE scope_id=? AND resource_type=? AND canonical_url=? AND version=?`, scope, typ, url, ver).Scan(&r.ScopeID, &r.ResourceType, &r.ResourceID, &r.CanonicalURL, &r.Version, &r.Status, &r.ResourceJSON, &r.ContentMode, &r.SourceModule)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
func (s *TerminologyStore) PutResource(ctx context.Context, r store.TerminologyResourceRecord) error {
	_, e := s.exec.ExecContext(ctx, `INSERT INTO terminology_resource(scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(scope_id,resource_type,canonical_url,version) DO UPDATE SET resource_id=excluded.resource_id,status=excluded.status,resource_json=excluded.resource_json,content_mode=excluded.content_mode,source_module=excluded.source_module,updated_at=CURRENT_TIMESTAMP`, r.ScopeID, r.ResourceType, r.ResourceID, r.CanonicalURL, r.Version, r.Status, r.ResourceJSON, r.ContentMode, r.SourceModule)
	return e
}
func (s *TerminologyStore) DeleteResource(ctx context.Context, scope, typ, url, ver string) error {
	_, e := s.exec.ExecContext(ctx, `DELETE FROM terminology_resource WHERE scope_id=? AND resource_type=? AND canonical_url=? AND version=?`, scope, typ, url, ver)
	return e
}
func (s *TerminologyStore) ListResources(ctx context.Context, scope, typ string) ([]store.TerminologyResourceRecord, error) {
	rows, e := s.exec.QueryContext(ctx, `SELECT scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module FROM terminology_resource WHERE scope_id=? AND (?='' OR resource_type=?) ORDER BY canonical_url,version`, scope, typ, typ)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []store.TerminologyResourceRecord
	for rows.Next() {
		var r store.TerminologyResourceRecord
		if e = rows.Scan(&r.ScopeID, &r.ResourceType, &r.ResourceID, &r.CanonicalURL, &r.Version, &r.Status, &r.ResourceJSON, &r.ContentMode, &r.SourceModule); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *TerminologyStore) ReplaceCodeSystem(ctx context.Context, scope, url, ver string, cs []store.TerminologyConceptRecord) error {
	if _, e := s.exec.ExecContext(ctx, `DELETE FROM terminology_concept WHERE scope_id=? AND system_url=? AND system_version=?`, scope, url, ver); e != nil {
		return e
	}
	for _, c := range cs {
		if _, e := s.exec.ExecContext(ctx, `INSERT INTO terminology_concept(scope_id,system_url,system_version,code,display,definition,active,abstract,parent_code,properties_json,designations_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, scope, url, ver, c.Code, c.Display, c.Definition, c.Active, c.Abstract, c.ParentCode, c.PropertiesJSON, c.DesignationsJSON); e != nil {
			return e
		}
	}
	return nil
}
func (s *TerminologyStore) LookupConcept(ctx context.Context, scope, url, ver, code string) (*store.TerminologyConceptRecord, error) {
	q := `SELECT scope_id,system_url,system_version,code,display,definition,active,abstract,parent_code,properties_json,designations_json FROM terminology_concept WHERE scope_id=? AND system_url=? AND system_version=? AND code=?`
	args := []any{scope, url, ver, code}
	if ver == "" {
		q = `SELECT c.scope_id,c.system_url,c.system_version,c.code,c.display,c.definition,c.active,c.abstract,c.parent_code,c.properties_json,c.designations_json FROM terminology_concept c LEFT JOIN terminology_resource r ON r.scope_id=c.scope_id AND r.resource_type='CodeSystem' AND r.canonical_url=c.system_url AND r.version=c.system_version WHERE c.scope_id=? AND c.system_url=? AND c.code=? AND c.active=1 AND COALESCE(r.status,'')!='retired' ORDER BY c.system_version DESC LIMIT 1`
		args = []any{scope, url, code}
	}
	var c store.TerminologyConceptRecord
	e := s.exec.QueryRowContext(ctx, q, args...).Scan(&c.ScopeID, &c.SystemURL, &c.SystemVersion, &c.Code, &c.Display, &c.Definition, &c.Active, &c.Abstract, &c.ParentCode, &c.PropertiesJSON, &c.DesignationsJSON)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &c, nil
}
func (s *TerminologyStore) ReplaceValueSet(ctx context.Context, r store.TerminologyValueSetRecord, ms []store.TerminologyExpansionMemberRecord) error {
	_, e := s.exec.ExecContext(ctx, `INSERT INTO terminology_valueset(scope_id,canonical_url,version,status,compose_json,expansion_json,expansion_timestamp,expansion_fingerprint) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(scope_id,canonical_url,version) DO UPDATE SET status=excluded.status,compose_json=excluded.compose_json,expansion_json=excluded.expansion_json,expansion_timestamp=excluded.expansion_timestamp,expansion_fingerprint=excluded.expansion_fingerprint`, r.ScopeID, r.CanonicalURL, r.Version, r.Status, r.ComposeJSON, r.ExpansionJSON, r.ExpansionTimestamp, r.ExpansionFingerprint)
	if e != nil {
		return e
	}
	_, e = s.exec.ExecContext(ctx, `DELETE FROM terminology_expansion_member WHERE scope_id=? AND valueset_url=? AND valueset_version=?`, r.ScopeID, r.CanonicalURL, r.Version)
	if e != nil {
		return e
	}
	for _, m := range ms {
		if _, e = s.exec.ExecContext(ctx, `INSERT INTO terminology_expansion_member(scope_id,valueset_url,valueset_version,system_url,system_version,code,display,inactive) VALUES(?,?,?,?,?,?,?,?)`, r.ScopeID, r.CanonicalURL, r.Version, m.SystemURL, m.SystemVersion, m.Code, m.Display, m.Inactive); e != nil {
			return e
		}
	}
	return nil
}
func (s *TerminologyStore) GetValueSet(ctx context.Context, scope, url, ver string) (*store.TerminologyValueSetRecord, error) {
	q := `SELECT scope_id,canonical_url,version,status,compose_json,expansion_json,expansion_timestamp,expansion_fingerprint FROM terminology_valueset WHERE scope_id=? AND canonical_url=? AND version=?`
	args := []any{scope, url, ver}
	if ver == "" {
		q = `SELECT scope_id,canonical_url,version,status,compose_json,expansion_json,expansion_timestamp,expansion_fingerprint FROM terminology_valueset WHERE scope_id=? AND canonical_url=? AND status!='retired' ORDER BY version DESC LIMIT 1`
		args = []any{scope, url}
	}
	var r store.TerminologyValueSetRecord
	e := s.exec.QueryRowContext(ctx, q, args...).Scan(&r.ScopeID, &r.CanonicalURL, &r.Version, &r.Status, &r.ComposeJSON, &r.ExpansionJSON, &r.ExpansionTimestamp, &r.ExpansionFingerprint)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, fmt.Errorf("get valueset: %w", e)
	}
	return &r, nil
}
func (s *TerminologyStore) ListValueSetMembers(ctx context.Context, scope, url, ver string) ([]store.TerminologyExpansionMemberRecord, error) {
	rows, e := s.exec.QueryContext(ctx, `SELECT scope_id,valueset_url,valueset_version,system_url,system_version,code,display,inactive FROM terminology_expansion_member WHERE scope_id=? AND valueset_url=? AND valueset_version=?`, scope, url, ver)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []store.TerminologyExpansionMemberRecord
	for rows.Next() {
		var m store.TerminologyExpansionMemberRecord
		if e = rows.Scan(&m.ScopeID, &m.ValueSetURL, &m.ValueSetVersion, &m.SystemURL, &m.SystemVersion, &m.Code, &m.Display, &m.Inactive); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *TerminologyStore) DeleteProjections(ctx context.Context, scope, typ, url, ver string) error {
	if typ == "CodeSystem" {
		_, e := s.exec.ExecContext(ctx, `DELETE FROM terminology_concept WHERE scope_id=? AND system_url=? AND system_version=?`, scope, url, ver)
		return e
	}
	_, e := s.exec.ExecContext(ctx, `DELETE FROM terminology_valueset WHERE scope_id=? AND canonical_url=? AND version=?`, scope, url, ver)
	return e
}
