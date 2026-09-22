package core_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/degoke/haistack/pkg/core"
	"github.com/degoke/haistack/pkg/search"
	"github.com/degoke/haistack/pkg/sqlite"
	"github.com/degoke/haistack/pkg/store"
	hasync "github.com/degoke/haistack/pkg/sync"
	"github.com/degoke/haistack/pkg/types"
)

func openSQLiteHarness(t *testing.T) *core.ResourceService {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "core.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: db.ResourceStore(),
		History:   db.HistoryStore(),
		Sessions:  db,
		IDPolicy:  core.DefaultIDPolicy{},
		Indexer:   &familyIndexer{},
		Outbox:    &hasync.EventStoreOutbox{Events: db.OutboxStore()},
	})
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}
	return svc
}

type familyIndexer struct{}

func (i *familyIndexer) Build(_ context.Context, resource *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(resource.JSON, &obj); err != nil {
		return nil, err
	}
	family := extractFamily(obj)
	if family == "" {
		return nil, nil
	}
	return []store.SearchIndexEntry{{
		ResourceType: resource.ResourceType,
		ID:           resource.ID,
		Fields:       map[string]string{"string.family": family},
	}}, nil
}

func extractFamily(obj map[string]interface{}) string {
	names, ok := obj["name"].([]interface{})
	if !ok || len(names) == 0 {
		return ""
	}
	first, ok := names[0].(map[string]interface{})
	if !ok {
		return ""
	}
	family, _ := first["family"].(string)
	return family
}

func TestSQLiteCoreCreateReadUpdateDelete(t *testing.T) {
	svc := openSQLiteHarness(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.VersionID == "" {
		t.Fatal("missing versionId")
	}

	read, err := svc.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != "pat-1" {
		t.Fatalf("Read ID = %q", read.ID)
	}

	updated, err := svc.Update(ctx, patientEnvelope("pat-1", "Smith"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VersionID == created.VersionID {
		t.Fatal("expected new version on update")
	}

	if err := svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = svc.Read(ctx, "Patient", "pat-1")
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

var _ search.Indexer = (*familyIndexer)(nil)
