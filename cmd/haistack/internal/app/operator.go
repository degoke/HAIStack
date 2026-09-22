package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

// ReindexPlanItem describes the resources a dry-run reindex would process.
type ReindexPlanItem struct {
	ResourceType string `json:"resourceType"`
	Count        int    `json:"count"`
}

// AuditStore returns the audit store for the session's configured backend.
func (s *Session) AuditStore() (store.AuditStore, error) {
	if s == nil || s.Runtime == nil {
		return nil, fmt.Errorf("session is not available")
	}
	p := s.Runtime.Persistence()
	switch {
	case p.SQLite != nil:
		return p.SQLite.AuditStore(), nil
	case p.TenantDB != nil:
		return p.TenantDB.AuditStore(), nil
	default:
		return nil, fmt.Errorf("no persistence backend available")
	}
}

// ExportResources reads current resources of one type. A zero limit exports all
// resources; a positive limit caps the result.
func (s *Session) ExportResources(ctx context.Context, resourceType string, limit int) ([]*types.ResourceEnvelope, error) {
	if limit < 0 {
		return nil, fmt.Errorf("export limit cannot be negative")
	}
	resources, _, err := s.resourceStores()
	if err != nil {
		return nil, err
	}
	const defaultBatchSize = 100
	batchSize := defaultBatchSize
	if limit > 0 && limit < batchSize {
		batchSize = limit
	}

	var out []*types.ResourceEnvelope
	offset := 0
	for {
		ids, err := resources.ListIDs(ctx, resourceType, batchSize, offset)
		if err != nil {
			return nil, fmt.Errorf("list %s resources: %w", resourceType, err)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			resource, err := resources.Read(ctx, resourceType, id)
			if err != nil {
				return nil, fmt.Errorf("read %s/%s: %w", resourceType, id, err)
			}
			out = append(out, resource)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		offset += len(ids)
	}
	return out, nil
}

// BackupManifest describes a directory of NDJSON resource dumps.
type BackupManifest struct {
	Format    string               `json:"format"`
	CreatedAt time.Time            `json:"createdAt"`
	Files     []BackupManifestFile `json:"files"`
}

// BackupManifestFile is one resource-type NDJSON file in a backup.
type BackupManifestFile struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	File  string `json:"file"`
}

// BackupReport is the CLI result of writing a backup directory.
type BackupReport struct {
	Directory string               `json:"directory"`
	Files     []BackupManifestFile `json:"files"`
}

const backupFormatV1 = "haistack-backup/v1"

// Backup writes one NDJSON file per resource type plus manifest.json.
func (s *Session) Backup(ctx context.Context, dir string) (*BackupReport, error) {
	if dir == "" {
		dir = "backup"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	manifest := BackupManifest{Format: backupFormatV1, CreatedAt: time.Now().UTC()}
	for _, resourceType := range s.backupResourceTypes() {
		resources, err := s.ExportResources(ctx, resourceType, 0)
		if err != nil {
			return nil, err
		}
		if len(resources) == 0 {
			continue
		}
		filename := resourceType + ".ndjson"
		var buf bytes.Buffer
		for i, resource := range resources {
			if i > 0 {
				_ = buf.WriteByte('\n')
			}
			_, _ = buf.Write(bytes.TrimSpace(resource.JSON))
		}
		if err := os.WriteFile(filepath.Join(dir, filename), buf.Bytes(), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", filename, err)
		}
		manifest.Files = append(manifest.Files, BackupManifestFile{
			Type:  resourceType,
			Count: len(resources),
			File:  filename,
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}
	return &BackupReport{Directory: dir, Files: manifest.Files}, nil
}

// RestoreReport is the CLI result of loading a backup directory.
type RestoreReport struct {
	Directory string `json:"directory"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Files     int    `json:"files"`
}

// Restore loads NDJSON files from a backup directory using create-or-update.
func (s *Session) Restore(ctx context.Context, dir string) (*RestoreReport, error) {
	if dir == "" {
		dir = "backup"
	}
	svc := s.Runtime.Services().ResourceService
	if svc == nil {
		return nil, fmt.Errorf("resource service is not available")
	}
	files, err := s.restoreFiles(dir)
	if err != nil {
		return nil, err
	}
	report := &RestoreReport{Directory: dir, Files: len(files)}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.File))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.File, err)
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			env, err := types.NewJSONCodec().ParseJSON(file.Type, line)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", file.File, i+1, err)
			}
			cleaned, err := types.SetMeta(env.JSON, types.Meta{})
			if err != nil {
				return nil, fmt.Errorf("%s line %d: clear meta: %w", file.File, i+1, err)
			}
			env.JSON = cleaned
			env.VersionID = ""
			action, _, err := ImportResource(ctx, svc, env, false, false)
			if err != nil {
				return nil, fmt.Errorf("%s %s/%s: %w", file.File, env.ResourceType, env.ID, err)
			}
			switch action {
			case "create":
				report.Created++
			case "update":
				report.Updated++
			}
		}
	}
	return report, nil
}

func (s *Session) backupResourceTypes() []string {
	if s != nil && s.Runtime != nil && s.Runtime.Services() != nil && s.Runtime.Services().RegistrySnapshot != nil {
		types := s.Runtime.Services().RegistrySnapshot.EnabledResourceTypes()
		if len(types) > 0 {
			return types
		}
	}
	return []string{"Patient", "Observation", "Encounter", "Practitioner", "Organization", "Appointment"}
}

func (s *Session) restoreFiles(dir string) ([]BackupManifestFile, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest BackupManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		return orderRestoreFiles(manifest.Files), nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}
	var files []BackupManifestFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".ndjson") {
			continue
		}
		files = append(files, BackupManifestFile{
			Type: strings.TrimSuffix(name, ".ndjson"),
			File: name,
		})
	}
	return orderRestoreFiles(files), nil
}

func orderRestoreFiles(files []BackupManifestFile) []BackupManifestFile {
	priority := map[string]int{
		"Organization": 0,
		"Practitioner": 1,
		"Patient":      2,
		"Encounter":    3,
	}
	out := append([]BackupManifestFile(nil), files...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, okI := priority[out[i].Type]
		if !okI {
			pi = 50
		}
		pj, okJ := priority[out[j].Type]
		if !okJ {
			pj = 50
		}
		if pi != pj {
			return pi < pj
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// ReindexPlan counts current resources for the selected enabled type(s) without
// modifying search indexes.
func (s *Session) ReindexPlan(ctx context.Context, resourceType string) ([]ReindexPlanItem, error) {
	if s == nil || s.Runtime == nil || s.Runtime.Services() == nil {
		return nil, fmt.Errorf("runtime services are not available")
	}
	if !s.Config.Runtime.EnableSearch {
		return nil, fmt.Errorf("search is not enabled in configuration")
	}
	snapshot := s.Runtime.Services().RegistrySnapshot
	if snapshot == nil {
		return nil, fmt.Errorf("registry snapshot is not available")
	}
	resourceTypes := []string{resourceType}
	if resourceType == "" {
		resourceTypes = resourceTypes[:0]
		for _, capability := range snapshot.CapabilitySnapshot().Resources {
			resourceTypes = append(resourceTypes, capability.ResourceType)
		}
		sort.Strings(resourceTypes)
	} else if !snapshot.IsResourceEnabled(resourceType) {
		return nil, fmt.Errorf("resource type %q is not enabled", resourceType)
	}
	resources, _, err := s.resourceStores()
	if err != nil {
		return nil, err
	}
	plan := make([]ReindexPlanItem, 0, len(resourceTypes))
	for _, typ := range resourceTypes {
		count := 0
		for offset := 0; ; offset += 100 {
			ids, err := resources.ListIDs(ctx, typ, 100, offset)
			if err != nil {
				return nil, fmt.Errorf("count %s resources: %w", typ, err)
			}
			count += len(ids)
			if len(ids) < 100 {
				break
			}
		}
		plan = append(plan, ReindexPlanItem{ResourceType: typ, Count: count})
	}
	return plan, nil
}
