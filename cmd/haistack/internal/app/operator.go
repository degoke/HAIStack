package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
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
