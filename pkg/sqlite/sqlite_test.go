package sqlite_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

var (
	_ store.ResourceStore        = (*sqlite.ResourceStore)(nil)
	_ store.HistoryStore         = (*sqlite.HistoryStore)(nil)
	_ store.SearchStore          = (*sqlite.SearchStore)(nil)
	_ store.EventStore           = (*sqlite.OutboxStore)(nil)
	_ store.CursorStore          = (*sqlite.CursorStore)(nil)
	_ store.ConflictStore        = (*sqlite.ConflictStore)(nil)
	_ store.BinaryStore          = (*sqlite.BinaryStore)(nil)
	_ store.ModuleStore          = (*sqlite.ModuleStore)(nil)
	_ store.DefinitionStore      = (*sqlite.DefinitionStore)(nil)
	_ store.RegistryInstallStore = (*sqlite.RegistryInstallStore)(nil)
	_ store.AuthStore            = (*sqlite.AuthStore)(nil)
	_ store.JobStore             = (*sqlite.JobStore)(nil)
	_ store.AuditStore           = (*sqlite.AuditStore)(nil)
	_ store.WriteSession         = (*sqlite.Session)(nil)
	_ store.WriteSessionProvider = (*sqlite.DB)(nil)
	_ store.InboxStore           = (*sqlite.InboxStore)(nil)
)

func openTestDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

func TestMigrationCreatesSchema(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))

	tables := []string{
		"resource", "resource_history",
		"search_token", "search_string", "search_date", "search_number", "search_reference",
		"sync_outbox", "sync_inbox_applied", "sync_cursor", "sync_conflict",
		"binary_object", "module_registry",
		"auth_principal", "auth_role", "auth_tenant_binding", "auth_device_identity", "auth_policy_document",
		"definition_resource", "definition_target", "registry_install",
		"schema_migrations",
	}
	for _, table := range tables {
		var name string
		err := db.SQL().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrationIdempotent(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("third Migrate: %v", err)
	}
}

func TestResourceStoreCRUD(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	resources := db.ResourceStore()

	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if err := resources.Create(ctx, envelope); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exists, err := resources.Exists(ctx, "Patient", "pat-1")
	if err != nil || !exists {
		t.Fatalf("Exists after create = %v, %v", exists, err)
	}

	read, err := resources.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.VersionID != "1" || string(read.JSON) != string(envelope.JSON) {
		t.Fatalf("Read = %+v, want version 1", read)
	}

	envelope.VersionID = "2"
	envelope.Hash = "hash-v2"
	if err := resources.Update(ctx, envelope); err != nil {
		t.Fatalf("Update: %v", err)
	}
	read, err = resources.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read after update: %v", err)
	}
	if read.VersionID != "2" {
		t.Fatalf("VersionID = %q, want 2", read.VersionID)
	}

	if err := resources.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = resources.Exists(ctx, "Patient", "pat-1")
	if err != nil || exists {
		t.Fatalf("Exists after delete = %v, %v", exists, err)
	}
}

func TestHistoryStoreAppendAndGet(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	history := db.HistoryStore()

	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     envelope,
		Hash:         "hash-v1",
	}); err != nil {
		t.Fatalf("AppendVersion create: %v", err)
	}

	deleteTS := now.Add(time.Minute)
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "2",
		Action:       store.VersionActionDelete,
		Timestamp:    deleteTS,
		Hash:         "hash-delete",
		Deleted:      true,
	}); err != nil {
		t.Fatalf("AppendVersion delete: %v", err)
	}

	versions, err := history.GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("history length = %d, want 2", len(versions))
	}
	if versions[0].Action != store.VersionActionCreate {
		t.Errorf("first action = %q, want create", versions[0].Action)
	}
	last := versions[1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last entry = %+v, want delete tombstone", last)
	}
	if last.Resource != nil {
		t.Error("delete tombstone should not carry resource payload")
	}
}

func TestAuthStoreSnapshotHydratesEngine(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	authStore := db.AuthStore("tenant-a")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := authStore.UpsertPrincipal(ctx, store.AuthPrincipalRecord{
		ID:          "user-1",
		Kind:        string(auth.KindUser),
		DisplayName: "Clinician",
		Attributes:  map[string]string{"department": "ward-a"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertPrincipal: %v", err)
	}
	if err := authStore.UpsertRole(ctx, store.AuthRoleRecord{
		Name:        "clinician",
		Permissions: []string{"appointment.read"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := authStore.UpsertTenantBinding(ctx, store.AuthTenantBindingRecord{
		PrincipalID: "user-1",
		Roles:       []string{"clinician"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertTenantBinding: %v", err)
	}
	if err := authStore.UpsertDevice(ctx, store.AuthDeviceRecord{
		DeviceID:  "device-1",
		Status:    auth.DeviceStatusActive,
		Trusted:   true,
		Metadata:  map[string]string{"model": "tablet"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}
	if err := authStore.UpsertPolicy(ctx, store.AuthPolicyRecord{
		Name:      "default",
		Format:    string(auth.PolicyFormatJSON),
		Version:   "1",
		Body:      []byte(`{"version":"1","rules":[{"name":"allow-appt-read","effect":"allow","match":{"actions":["read"],"resourceTypes":["Appointment"],"anyPermissions":["appointment.read"]},"reason":"allow appointment read"},{"name":"allow-device-push","effect":"allow","match":{"actions":["push-device-event"],"deviceTrusted":true,"deviceStatuses":["active"]},"reason":"allow device push"}]}`),
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}

	engine, err := auth.NewEngineFromStore(ctx, authStore)
	if err != nil {
		t.Fatalf("NewEngineFromStore: %v", err)
	}
	principal, err := engine.Catalog().GetPrincipal("user-1")
	if err != nil {
		t.Fatalf("GetPrincipal: %v", err)
	}
	decision, err := engine.CanReadResource(ctx, auth.ReadRequest{
		Principal:    principal,
		Tenant:       auth.TenantContext{TenantID: "tenant-a"},
		ResourceType: "Appointment",
	})
	if err != nil {
		t.Fatalf("CanReadResource: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed", decision)
	}
	deviceDecision, err := engine.CanPushDeviceEvent(ctx, auth.DevicePushRequest{
		DeviceID: "device-1",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("CanPushDeviceEvent: %v", err)
	}
	if !deviceDecision.Allowed {
		t.Fatalf("device decision = %#v, want allowed", deviceDecision)
	}
}

func TestOutboxStoreAppendAndReadSince(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	outbox := db.OutboxStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	e1, err := outbox.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		Action:       store.EventActionCreate,
		Timestamp:    now,
		Hash:         "h1",
	})
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if e1.Sequence != 1 {
		t.Errorf("sequence 1 = %d, want 1", e1.Sequence)
	}

	e2, err := outbox.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "pat-2",
		VersionID:    "1",
		Action:       store.EventActionCreate,
		Timestamp:    now,
		Hash:         "h2",
	})
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if e2.Sequence != 2 {
		t.Errorf("sequence 2 = %d, want 2", e2.Sequence)
	}

	all, err := outbox.ReadSince(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ReadSince all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all events = %d, want 2", len(all))
	}

	afterFirst, err := outbox.ReadSince(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ReadSince after 1: %v", err)
	}
	if len(afterFirst) != 1 || afterFirst[0].ID != "pat-2" {
		t.Fatalf("after first = %+v, want pat-2", afterFirst)
	}
}

func TestCursorStoreUpsertGetDelete(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	cursors := db.CursorStore()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	got, err := cursors.GetCursor(ctx, "sync-worker")
	if err != nil {
		t.Fatalf("GetCursor missing: %v", err)
	}
	if got != nil {
		t.Fatalf("GetCursor = %+v, want nil", got)
	}

	if err := cursors.UpsertCursor(ctx, store.Cursor{
		Name:      "sync-worker",
		Position:  "42",
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertCursor: %v", err)
	}

	got, err = cursors.GetCursor(ctx, "sync-worker")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if got == nil || got.Position != "42" {
		t.Fatalf("GetCursor = %+v, want position 42", got)
	}

	if err := cursors.UpsertCursor(ctx, store.Cursor{
		Name:      "sync-worker",
		Position:  "99",
		UpdatedAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertCursor update: %v", err)
	}
	got, err = cursors.GetCursor(ctx, "sync-worker")
	if err != nil {
		t.Fatalf("GetCursor after update: %v", err)
	}
	if got.Position != "99" {
		t.Fatalf("Position = %q, want 99", got.Position)
	}

	if err := cursors.DeleteCursor(ctx, "sync-worker"); err != nil {
		t.Fatalf("DeleteCursor: %v", err)
	}
	got, err = cursors.GetCursor(ctx, "sync-worker")
	if err != nil {
		t.Fatalf("GetCursor after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("GetCursor after delete = %+v, want nil", got)
	}
}

func TestSearchStoreIndexLookupRemove(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	search := db.SearchStore()

	if err := search.Index(ctx, store.SearchIndexEntry{
		ResourceType: "Patient",
		ID:           "pat-1",
		Fields: map[string]string{
			"string.family": "Doe",
			"token.gender":  "female",
		},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	ids, err := search.Lookup(ctx, "string.family", "Doe")
	if err != nil {
		t.Fatalf("Lookup string: %v", err)
	}
	if len(ids) != 1 || ids[0] != "pat-1" {
		t.Fatalf("Lookup string ids = %v", ids)
	}

	ids, err = search.Lookup(ctx, "token.gender", "female")
	if err != nil {
		t.Fatalf("Lookup token: %v", err)
	}
	if len(ids) != 1 || ids[0] != "pat-1" {
		t.Fatalf("Lookup token ids = %v", ids)
	}

	ids, err = search.QueryPrepared(ctx, store.PreparedQuery{Name: "by-field"}, map[string]string{
		"key":   "string.family",
		"value": "Doe",
	})
	if err != nil {
		t.Fatalf("QueryPrepared: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("QueryPrepared ids = %v", ids)
	}

	if err := search.RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}
	ids, err = search.Lookup(ctx, "string.family", "Doe")
	if err != nil {
		t.Fatalf("Lookup after remove: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", ids)
	}
}

func TestApplyLocalWriteAtomic(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	result, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		SearchEntries: []store.SearchIndexEntry{{
			ResourceType: "Patient",
			ID:           "pat-1",
			Fields:       map[string]string{"string.family": "Doe"},
		}},
		Event: store.ResourceEvent{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.EventActionCreate,
			Timestamp:    now,
			Hash:         "hash-v1",
		},
		Version: store.ResourceVersion{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     envelope,
			Hash:         "hash-v1",
		},
	})
	if err != nil {
		t.Fatalf("ApplyLocalWrite: %v", err)
	}
	if result.Event.Sequence != 1 {
		t.Errorf("event sequence = %d, want 1", result.Event.Sequence)
	}

	exists, err := db.ResourceStore().Exists(ctx, "Patient", "pat-1")
	if err != nil || !exists {
		t.Fatalf("resource exists = %v, %v", exists, err)
	}
	history, err := db.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	events, err := db.OutboxStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	ids, err := db.SearchStore().Lookup(ctx, "string.family", "Doe")
	if err != nil || len(ids) != 1 {
		t.Fatalf("search ids = %v, %v", ids, err)
	}
}

func TestApplyLocalWriteRollbackOnError(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}
	if _, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Event: store.ResourceEvent{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.EventActionCreate,
			Timestamp:    now,
		},
		Version: store.ResourceVersion{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     envelope,
		},
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	// Duplicate create should fail and roll back entirely.
	_, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Event: store.ResourceEvent{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "2",
			Action:       store.EventActionCreate,
			Timestamp:    now,
		},
		Version: store.ResourceVersion{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "2",
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     envelope,
		},
	})
	if err == nil {
		t.Fatal("expected duplicate create to fail")
	}

	history, err := db.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1 (rolled back second write)", len(history))
	}
	events, err := db.OutboxStore().ReadSince(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events length = %d, want 1", len(events))
	}
}

func TestApplyLocalWriteDelete(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if _, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		SearchEntries: []store.SearchIndexEntry{{
			ResourceType: "Patient",
			ID:           "pat-1",
			Fields:       map[string]string{"string.family": "Doe"},
		}},
		Event: store.ResourceEvent{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.EventActionCreate,
			Timestamp:    now,
			Hash:         "hash-v1",
		},
		Version: store.ResourceVersion{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "1",
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     envelope,
			Hash:         "hash-v1",
		},
	}); err != nil {
		t.Fatalf("create write: %v", err)
	}

	deleteTS := now.Add(time.Minute)
	_, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: envelope,
		Action:   store.VersionActionDelete,
		Event: store.ResourceEvent{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "2",
			Action:       store.EventActionDelete,
			Timestamp:    deleteTS,
			Hash:         "hash-delete",
		},
		Version: store.ResourceVersion{
			ResourceType: "Patient",
			ID:           "pat-1",
			VersionID:    "2",
			Action:       store.VersionActionDelete,
			Timestamp:    deleteTS,
			Hash:         "hash-delete",
			Deleted:      true,
		},
	})
	if err != nil {
		t.Fatalf("delete write: %v", err)
	}

	exists, err := db.ResourceStore().Exists(ctx, "Patient", "pat-1")
	if err != nil || exists {
		t.Fatalf("resource should be deleted: exists=%v err=%v", exists, err)
	}
	history, err := db.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	last := history[len(history)-1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last history = %+v", last)
	}
	events, err := db.OutboxStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	ids, err := db.SearchStore().Lookup(ctx, "string.family", "Doe")
	if err != nil || len(ids) != 0 {
		t.Fatalf("search should be cleared: %v, %v", ids, err)
	}
}

func TestInboxStoreMarkApplied(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	inbox := db.InboxStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	applied, err := inbox.IsApplied(ctx, "remote-op-1")
	if err != nil {
		t.Fatalf("IsApplied: %v", err)
	}
	if applied {
		t.Fatal("expected not applied")
	}

	if err := inbox.MarkApplied(ctx, "remote-op-1", now); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	applied, err = inbox.IsApplied(ctx, "remote-op-1")
	if err != nil || !applied {
		t.Fatalf("IsApplied after mark = %v, %v", applied, err)
	}
	ts, err := inbox.AppliedAt(ctx, "remote-op-1")
	if err != nil || ts == nil {
		t.Fatalf("AppliedAt = %v, %v", ts, err)
	}
}

func TestSessionCommitAndRollback(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()

	session, err := db.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	session, err = db.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := session.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestConcurrentReadWriteSmoke(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	resources := db.ResourceStore()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("pat-%d", i)
			now := time.Now().UTC()
			env := &types.ResourceEnvelope{
				ResourceType: "Patient",
				ID:           id,
				VersionID:    "1",
				LastUpdated:  now,
				JSON:         []byte(`{"resourceType":"Patient","id":"` + id + `"}`),
				Hash:         "hash",
			}
			if err := resources.Create(ctx, env); err != nil {
				errs <- err
				return
			}
			_, err := resources.Read(ctx, "Patient", id)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent error: %v", err)
		}
	}
}

func TestMemoryDatabase(t *testing.T) {
	db := openTestDB(t, ":memory:")
	ctx := context.Background()
	if err := db.ResourceStore().Create(ctx, &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "mem-1",
		VersionID:    "1",
		LastUpdated:  time.Now().UTC(),
		JSON:         []byte(`{"resourceType":"Patient","id":"mem-1"}`),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestOpenWithOptions(t *testing.T) {
	path := tempDBPath(t)
	db, err := sqlite.Open(path, sqlite.WithBusyTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestComposeResourceHistoryEventAndSearch(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	resources := db.ResourceStore()
	history := db.HistoryStore()
	events := db.OutboxStore()
	search := db.SearchStore()

	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if err := resources.Create(ctx, envelope); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		VersionID:    envelope.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     envelope,
		Hash:         envelope.Hash,
	}); err != nil {
		t.Fatalf("AppendVersion: %v", err)
	}
	storedEvent, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		VersionID:    envelope.VersionID,
		Action:       store.EventActionCreate,
		Timestamp:    now,
		Hash:         envelope.Hash,
	})
	if err != nil {
		t.Fatalf("Append event: %v", err)
	}
	if storedEvent.Sequence != 1 {
		t.Errorf("event sequence = %d, want 1", storedEvent.Sequence)
	}
	if err := search.Index(ctx, store.SearchIndexEntry{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		Fields:       map[string]string{"family": "Doe"},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	exists, err := resources.Exists(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("expected resource to exist")
	}

	versions, err := history.GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("history length = %d, want 1", len(versions))
	}

	readEvents, err := events.ReadSince(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(readEvents) != 1 {
		t.Fatalf("events length = %d, want 1", len(readEvents))
	}

	ids, err := search.Lookup(ctx, "family", "Doe")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(ids) != 1 || ids[0] != "pat-1" {
		t.Fatalf("Lookup ids = %v, want [pat-1]", ids)
	}

	if err := resources.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleteVersion := store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "2",
		Action:       store.VersionActionDelete,
		Timestamp:    now.Add(time.Minute),
		Hash:         "hash-delete",
		Deleted:      true,
	}
	if err := history.AppendVersion(ctx, deleteVersion); err != nil {
		t.Fatalf("AppendVersion tombstone: %v", err)
	}
	if _, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "2",
		Action:       store.EventActionDelete,
		Timestamp:    deleteVersion.Timestamp,
		Hash:         deleteVersion.Hash,
	}); err != nil {
		t.Fatalf("Append delete event: %v", err)
	}
	if err := search.RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}

	exists, err = resources.Exists(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("expected resource to be deleted from current state")
	}

	versions, err = history.GetHistory(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("GetHistory after delete: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("history length = %d, want 2", len(versions))
	}
	last := versions[len(versions)-1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last history entry = %+v, want delete tombstone", last)
	}
}

func TestConflictStoreAppendListResolve(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	conflicts := db.ConflictStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	record := store.ConflictRecord{
		ID:              "conf-1",
		ResourceType:    "Patient",
		ResourceID:      "pat-1",
		LocalVersionID:  "2",
		RemoteVersionID: "3",
		Reason:          "version mismatch",
		CreatedAt:       now,
	}
	if err := conflicts.Append(ctx, record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	list, err := conflicts.List(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "conf-1" {
		t.Fatalf("List = %+v", list)
	}

	resolvedAt := now.Add(time.Hour)
	if err := conflicts.Resolve(ctx, "conf-1", resolvedAt); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	list, err = conflicts.List(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("List after resolve: %v", err)
	}
	if list[0].ResolvedAt == nil || !list[0].ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("ResolvedAt = %v, want %v", list[0].ResolvedAt, resolvedAt)
	}
}

func TestBinaryStorePutGetDelete(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	binaries := db.BinaryStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	obj := store.BinaryObject{
		Key:         "thumb-1",
		ContentType: "image/png",
		Size:        4,
		Hash:        "abc",
		Data:        []byte{1, 2, 3, 4},
		CreatedAt:   now,
	}
	if err := binaries.Put(ctx, obj); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := binaries.Get(ctx, "thumb-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ContentType != "image/png" || got.Size != 4 || len(got.Data) != 4 {
		t.Fatalf("Get = %+v", got)
	}

	if err := binaries.Delete(ctx, "thumb-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := binaries.Get(ctx, "thumb-1"); err == nil {
		t.Fatal("expected get after delete to fail")
	}
}

func TestModuleStoreRegisterGetListUnregister(t *testing.T) {
	db := openTestDB(t, tempDBPath(t))
	ctx := context.Background()
	modules := db.ModuleStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	mod := store.ModuleRecord{
		Name:         "vitals-widget",
		Version:      "1.0.0",
		Metadata:     map[string]string{"channel": "stable"},
		RegisteredAt: now,
	}
	if err := modules.Register(ctx, mod); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := modules.Get(ctx, "vitals-widget")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != "1.0.0" || got.Metadata["channel"] != "stable" {
		t.Fatalf("Get = %+v", got)
	}

	list, err := modules.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List length = %d, want 1", len(list))
	}

	if err := modules.Unregister(ctx, "vitals-widget"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	list, err = modules.List(ctx)
	if err != nil {
		t.Fatalf("List after unregister: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after unregister = %d, want 0", len(list))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
