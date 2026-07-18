package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5"
)

type TerminologyStore struct {
	exec  querier
	scope string
}

func newTerminologyStore(e querier, scope string) *TerminologyStore {
	return &TerminologyStore{exec: e, scope: scope}
}
func (s *TerminologyStore) FindResource(ctx context.Context, scope, typ, url, ver string) (*store.TerminologyResourceRecord, error) {
	var r store.TerminologyResourceRecord
	err := s.exec.QueryRow(ctx, `SELECT scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module FROM terminology_resource WHERE scope_id=$1 AND resource_type=$2 AND canonical_url=$3 AND version=$4`, scope, typ, url, ver).Scan(&r.ScopeID, &r.ResourceType, &r.ResourceID, &r.CanonicalURL, &r.Version, &r.Status, &r.ResourceJSON, &r.ContentMode, &r.SourceModule)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
func (s *TerminologyStore) PutResource(ctx context.Context, r store.TerminologyResourceRecord) error {
	_, err := s.exec.Exec(ctx, `INSERT INTO terminology_resource(scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9) ON CONFLICT(scope_id,resource_type,canonical_url,version) DO UPDATE SET resource_id=excluded.resource_id,status=excluded.status,resource_json=excluded.resource_json,content_mode=excluded.content_mode,source_module=excluded.source_module,updated_at=now()`, r.ScopeID, r.ResourceType, r.ResourceID, r.CanonicalURL, r.Version, r.Status, r.ResourceJSON, r.ContentMode, r.SourceModule)
	return err
}
func (s *TerminologyStore) DeleteResource(ctx context.Context, scope, typ, url, ver string) error {
	_, e := s.exec.Exec(ctx, `DELETE FROM terminology_resource WHERE scope_id=$1 AND resource_type=$2 AND canonical_url=$3 AND version=$4`, scope, typ, url, ver)
	return e
}
func (s *TerminologyStore) ListResources(ctx context.Context, scope, typ string) ([]store.TerminologyResourceRecord, error) {
	rows, e := s.exec.Query(ctx, `SELECT scope_id,resource_type,resource_id,canonical_url,version,status,resource_json,content_mode,source_module FROM terminology_resource WHERE scope_id=$1 AND ($2='' OR resource_type=$2) ORDER BY canonical_url,version`, scope, typ)
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
	if _, e := s.exec.Exec(ctx, `DELETE FROM terminology_concept WHERE scope_id=$1 AND system_url=$2 AND system_version=$3`, scope, url, ver); e != nil {
		return e
	}
	for _, c := range cs {
		if _, e := s.exec.Exec(ctx, `INSERT INTO terminology_concept(scope_id,system_url,system_version,code,display,definition,active,abstract,parent_code,properties_json,designations_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb)`, scope, url, ver, c.Code, c.Display, c.Definition, c.Active, c.Abstract, c.ParentCode, jsonOrEmpty(c.PropertiesJSON), jsonOrEmpty(c.DesignationsJSON)); e != nil {
			return e
		}
	}
	return nil
}
func jsonOrEmpty(v string) string {
	if v == "" {
		return "{}"
	}
	return v
}
func (s *TerminologyStore) LookupConcept(ctx context.Context, scope, url, ver, code string) (*store.TerminologyConceptRecord, error) {
	q := `SELECT scope_id,system_url,system_version,code,display,definition,active,abstract,parent_code,COALESCE(properties_json::text,'{}'),COALESCE(designations_json::text,'{}') FROM terminology_concept WHERE scope_id=$1 AND system_url=$2 AND system_version=$3 AND code=$4`
	args := []any{scope, url, ver, code}
	if ver == "" {
		q = `SELECT c.scope_id,c.system_url,c.system_version,c.code,c.display,c.definition,c.active,c.abstract,c.parent_code,COALESCE(c.properties_json::text,'{}'),COALESCE(c.designations_json::text,'{}') FROM terminology_concept c LEFT JOIN terminology_resource r ON r.scope_id=c.scope_id AND r.resource_type='CodeSystem' AND r.canonical_url=c.system_url AND r.version=c.system_version WHERE c.scope_id=$1 AND c.system_url=$2 AND c.code=$3 AND c.active AND COALESCE(r.status,'')!='retired' ORDER BY c.system_version DESC LIMIT 1`
		args = []any{scope, url, code}
	}
	var c store.TerminologyConceptRecord
	e := s.exec.QueryRow(ctx, q, args...).Scan(&c.ScopeID, &c.SystemURL, &c.SystemVersion, &c.Code, &c.Display, &c.Definition, &c.Active, &c.Abstract, &c.ParentCode, &c.PropertiesJSON, &c.DesignationsJSON)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &c, nil
}
func (s *TerminologyStore) ReplaceValueSet(ctx context.Context, r store.TerminologyValueSetRecord, ms []store.TerminologyExpansionMemberRecord) error {
	if _, e := s.exec.Exec(ctx, `INSERT INTO terminology_valueset(scope_id,canonical_url,version,status,compose_json,expansion_json,expansion_timestamp,expansion_fingerprint) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,NULLIF($7,'')::timestamptz,$8) ON CONFLICT(scope_id,canonical_url,version) DO UPDATE SET status=excluded.status,compose_json=excluded.compose_json,expansion_json=excluded.expansion_json,expansion_timestamp=excluded.expansion_timestamp,expansion_fingerprint=excluded.expansion_fingerprint`, r.ScopeID, r.CanonicalURL, r.Version, r.Status, jsonOrEmpty(r.ComposeJSON), jsonOrEmpty(string(r.ExpansionJSON)), r.ExpansionTimestamp, r.ExpansionFingerprint); e != nil {
		return e
	}
	if _, e := s.exec.Exec(ctx, `DELETE FROM terminology_expansion_member WHERE scope_id=$1 AND valueset_url=$2 AND valueset_version=$3`, r.ScopeID, r.CanonicalURL, r.Version); e != nil {
		return e
	}
	for _, m := range ms {
		if _, e := s.exec.Exec(ctx, `INSERT INTO terminology_expansion_member(scope_id,valueset_url,valueset_version,system_url,system_version,code,display,inactive) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.ScopeID, r.CanonicalURL, r.Version, m.SystemURL, m.SystemVersion, m.Code, m.Display, m.Inactive); e != nil {
			return e
		}
	}
	return nil
}
func (s *TerminologyStore) GetValueSet(ctx context.Context, scope, url, ver string) (*store.TerminologyValueSetRecord, error) {
	q := `SELECT scope_id,canonical_url,version,status,compose_json::text,COALESCE(expansion_json::text,''),COALESCE(expansion_timestamp::text,''),expansion_fingerprint FROM terminology_valueset WHERE scope_id=$1 AND canonical_url=$2 AND version=$3`
	args := []any{scope, url, ver}
	if ver == "" {
		q = `SELECT scope_id,canonical_url,version,status,compose_json::text,COALESCE(expansion_json::text,''),COALESCE(expansion_timestamp::text,''),expansion_fingerprint FROM terminology_valueset WHERE scope_id=$1 AND canonical_url=$2 AND status!='retired' ORDER BY version DESC LIMIT 1`
		args = []any{scope, url}
	}
	var r store.TerminologyValueSetRecord
	var exp string
	e := s.exec.QueryRow(ctx, q, args...).Scan(&r.ScopeID, &r.CanonicalURL, &r.Version, &r.Status, &r.ComposeJSON, &exp, &r.ExpansionTimestamp, &r.ExpansionFingerprint)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, fmt.Errorf("get valueset: %w", e)
	}
	r.ExpansionJSON = []byte(exp)
	return &r, nil
}
func (s *TerminologyStore) ListValueSetMembers(ctx context.Context, scope, url, ver string) ([]store.TerminologyExpansionMemberRecord, error) {
	rows, e := s.exec.Query(ctx, `SELECT scope_id,valueset_url,valueset_version,system_url,system_version,code,display,inactive FROM terminology_expansion_member WHERE scope_id=$1 AND valueset_url=$2 AND valueset_version=$3`, scope, url, ver)
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
	table := `terminology_valueset`
	if typ == "CodeSystem" {
		table = `terminology_concept`
	}
	q := fmt.Sprintf(`DELETE FROM %s WHERE scope_id=$1 AND %s=$2 AND %s=$3`, table, map[bool]string{true: "system_url", false: "canonical_url"}[typ == "CodeSystem"], map[bool]string{true: "system_version", false: "version"}[typ == "CodeSystem"])
	_, e := s.exec.Exec(ctx, q, scope, url, ver)
	return e
}

var _ store.TerminologyStore = (*TerminologyStore)(nil)
