package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestEnrichLocalEventCreate(t *testing.T) {
	ctx := context.Background()
	history := newMemHistoryStore()
	res := sampleResource("p1", "v1")
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    res.LastUpdated,
		Resource:     res,
		Hash:         res.Hash,
	})

	event := store.ResourceEvent{
		Sequence:     1,
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "v1",
		Action:       store.EventActionCreate,
		Timestamp:    res.LastUpdated,
		Hash:         res.Hash,
	}

	local, err := hasync.EnrichLocalEvent(ctx, event, "node-a", "tenant-a", history)
	if err != nil {
		t.Fatalf("EnrichLocalEvent: %v", err)
	}
	if local.Operation != hasync.EventTypeResourceCreated {
		t.Fatalf("operation = %q, want created", local.Operation)
	}
	if local.BaseCloudVersion != "" {
		t.Fatalf("base version = %q, want empty for create", local.BaseCloudVersion)
	}
	if local.ResourceAfter == nil || local.ResourceAfter.ID != "p1" {
		t.Fatalf("resource after not populated")
	}
}

func TestEnrichLocalEventUpdateWithBaseVersion(t *testing.T) {
	ctx := context.Background()
	history := newMemHistoryStore()
	base := sampleResource("p1", "cloud-v1")
	updated := sampleResource("p1", "local-v2")
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    base.LastUpdated,
		Resource:     base,
	})
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: updated.ResourceType,
		ID:           updated.ID,
		VersionID:    updated.VersionID,
		Action:       store.VersionActionUpdate,
		Timestamp:    updated.LastUpdated,
		Resource:     updated,
	})

	event := store.ResourceEvent{
		Sequence:     2,
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "local-v2",
		Action:       store.EventActionUpdate,
		Timestamp:    updated.LastUpdated,
		Hash:         updated.Hash,
	}

	local, err := hasync.EnrichLocalEvent(ctx, event, "node-a", "tenant-a", history)
	if err != nil {
		t.Fatalf("EnrichLocalEvent: %v", err)
	}
	if local.BaseCloudVersion != "cloud-v1" {
		t.Fatalf("base = %q, want cloud-v1", local.BaseCloudVersion)
	}
}

func TestEnrichLocalEventDeleteTombstone(t *testing.T) {
	ctx := context.Background()
	history := newMemHistoryStore()
	base := sampleResource("p1", "cloud-v1")
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    base.LastUpdated,
		Resource:     base,
	})
	deleteVersion := "tombstone-v2"
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    deleteVersion,
		Action:       store.VersionActionDelete,
		Timestamp:    time.Now().UTC(),
		Hash:         "tomb-hash",
		Deleted:      true,
	})

	event := store.ResourceEvent{
		Sequence:     2,
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    deleteVersion,
		Action:       store.EventActionDelete,
		Timestamp:    time.Now().UTC(),
		Hash:         "tomb-hash",
	}

	local, err := hasync.EnrichLocalEvent(ctx, event, "node-a", "tenant-a", history)
	if err != nil {
		t.Fatalf("EnrichLocalEvent: %v", err)
	}
	if local.Operation != hasync.EventTypeResourceDeleted {
		t.Fatalf("operation = %q", local.Operation)
	}
	if local.ResourceAfter != nil {
		t.Fatalf("delete tombstone should not include resource payload")
	}
	if local.BaseCloudVersion != "cloud-v1" {
		t.Fatalf("base = %q, want cloud-v1", local.BaseCloudVersion)
	}
}
