package core

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

// HistoryQuery filters instance history by FHIR _since / _at.
type HistoryQuery struct {
	Since *time.Time
	At    *time.Time
}

// VRead returns a specific historical version of a resource.
// Deleted versions return ErrorKindGone (HTTP 410).
func (s *ResourceService) VRead(ctx context.Context, resourceType, id, versionID string) (*types.ResourceEnvelope, error) {
	if resourceType == "" || id == "" || versionID == "" {
		return nil, invalidErr("resourceType, id, and versionId are required", nil)
	}
	version, err := s.history.GetVersion(ctx, resourceType, id, versionID)
	if err != nil {
		if isStoreNotFound(err) {
			return nil, notFoundErr(fmt.Sprintf("resource version not found: %s/%s/_history/%s", resourceType, id, versionID), nil)
		}
		return nil, exceptionErr("get resource version", err)
	}
	if version.Deleted || version.Action == store.VersionActionDelete {
		return nil, goneErr(fmt.Sprintf("resource version was deleted: %s/%s/_history/%s", resourceType, id, versionID), nil)
	}
	if version.Resource == nil {
		return nil, notFoundErr(fmt.Sprintf("resource version not found: %s/%s/_history/%s", resourceType, id, versionID), nil)
	}
	return version.Resource, nil
}

// FilterHistory applies FHIR instance-history _since and _at filters.
// Versions are assumed oldest-first. _since keeps versions created at or after
// that instant. _at keeps the version that was current at that instant.
func FilterHistory(versions []store.ResourceVersion, q HistoryQuery) []store.ResourceVersion {
	if q.Since == nil && q.At == nil {
		return versions
	}
	out := versions
	if q.Since != nil {
		filtered := make([]store.ResourceVersion, 0, len(out))
		since := q.Since.UTC()
		for _, version := range out {
			ts := version.Timestamp.UTC()
			if !ts.Before(since) {
				filtered = append(filtered, version)
			}
		}
		out = filtered
	}
	if q.At != nil {
		at := q.At.UTC()
		var current *store.ResourceVersion
		for i := range out {
			ts := out[i].Timestamp.UTC()
			if ts.After(at) {
				break
			}
			version := out[i]
			current = &version
		}
		if current == nil {
			return nil
		}
		return []store.ResourceVersion{*current}
	}
	return out
}
