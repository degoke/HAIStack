package postgres_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	_ store.ResourceStore          = (*postgres.ResourceStore)(nil)
	_ store.HistoryStore           = (*postgres.HistoryStore)(nil)
	_ store.SearchStore            = (*postgres.SearchStore)(nil)
	_ store.SearchQueryExecutor    = (*postgres.SearchStore)(nil)
	_ store.SearchAdvancedExecutor = (*postgres.SearchStore)(nil)
	_ store.SearchIndexMaintainer  = (*postgres.SearchStore)(nil)
	_ store.EventStore             = (*postgres.EventStore)(nil)
	_ store.CursorStore            = (*postgres.CursorStore)(nil)
	_ store.ConflictStore          = (*postgres.ConflictStore)(nil)
	_ store.IDRegistryStore        = (*postgres.IDRegistry)(nil)
	_ store.BinaryStore            = (*postgres.BinaryStore)(nil)
	_ store.BlobStore              = (*postgres.BlobStore)(nil)
	_ store.AuditStore             = (*postgres.AuditStore)(nil)
	_ store.SubscriptionStore      = (*postgres.SubscriptionStore)(nil)
	_ store.SubscriptionDeliveryStore = (*postgres.SubscriptionDeliveryStore)(nil)
	_ store.ModuleStore            = (*postgres.ModuleStore)(nil)
	_ store.MaterializedViewStore  = (*postgres.MaterializedViewStore)(nil)
	_ store.ReportingTableStore    = (*postgres.ReportingTableStore)(nil)
	_ store.AnalyticsStore         = (*postgres.AnalyticsStore)(nil)
	_ store.JobStore               = (*postgres.JobStore)(nil)
	_ store.NodeRegistryStore      = (*postgres.NodeRegistry)(nil)
	_ store.DefinitionStore        = (*postgres.DefinitionStore)(nil)
	_ store.RegistryInstallStore   = (*postgres.RegistryInstallStore)(nil)
	_ store.AuthStore              = (*postgres.AuthStore)(nil)
	_ store.WriteSession           = (*postgres.Session)(nil)
	_ store.WriteSessionProvider   = (*postgres.TenantDB)(nil)
	_ store.InboxStore             = (*postgres.InboxStore)(nil)
	_ binary.MetadataStore         = (*postgres.BlobMetadataStore)(nil)
	_ binary.TransferStore         = (*postgres.BlobMetadataStore)(nil)
	_ binary.BlobStore             = (*postgres.BlobChunkStore)(nil)
	_ binary.ChunkStore            = (*postgres.BlobChunkStore)(nil)
	_ binary.WriteSessionExtension = (*postgres.Session)(nil)
)

func initDockerHost() {
	if os.Getenv("DOCKER_HOST") != "" {
		return
	}
	out, err := exec.Command("docker", "context", "inspect", "-f", "{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return
	}
	if host := strings.TrimSpace(string(out)); host != "" {
		_ = os.Setenv("DOCKER_HOST", host)
	}
}

func dockerAvailable() bool {
	initDockerHost()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

func openTestDB(t *testing.T) (*postgres.DB, func()) {
	t.Helper()
	ctx := context.Background()

	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open TEST_POSTGRES_DSN: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			db.Close()
			t.Fatalf("Migrate: %v", err)
		}
		return db, db.Close
	}

	if !dockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("haistack_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("postgres unavailable (set TEST_POSTGRES_DSN or start Docker): %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}

	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("Migrate: %v", err)
	}

	cleanup := func() {
		db.Close()
		_ = container.Terminate(ctx)
	}
	return db, cleanup
}

func testTenant(t *testing.T, db *postgres.DB, suffix string) *postgres.TenantDB {
	t.Helper()
	tenantID := fmt.Sprintf("tenant-%s-%s", t.Name(), suffix)
	if err := db.EnsureTenant(context.Background(), tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	return db.Tenant(tenantID)
}

func TestMigrationCreatesSchema(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tables := []string{
		"tenant", "resource", "resource_history", "event_log",
		"resource_id_registry", "node_registry", "sync_conflict",
		"search_token", "search_string", "search_date", "search_number", "search_reference",
		"search_text", "search_composite",
		"sync_cursor", "sync_inbox_applied", "binary_object", "audit_log", "module_registry",
		"auth_principal", "auth_role", "auth_tenant_binding", "auth_device_identity", "auth_policy_document",
		"materialized_view", "analytics_reporting_meta", "analytics_reporting_row",
		"analytics_event", "background_job", "schema_migrations",
	}
	ctx := context.Background()
	for _, table := range tables {
		var name string
		err := db.Pool().QueryRow(ctx, `
			SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename = $1`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrationIdempotent(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("third Migrate: %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := testTenant(t, db, "a")
	tenantB := testTenant(t, db, "b")

	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		VersionID:    "1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if err := tenantA.ResourceStore().Create(ctx, envelope); err != nil {
		t.Fatalf("tenant A create: %v", err)
	}

	exists, err := tenantB.ResourceStore().Exists(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("tenant B exists: %v", err)
	}
	if exists {
		t.Fatal("tenant B should not see tenant A resource")
	}
}

func TestResourceStoreCRUD(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "resource")
	resources := tdb.ResourceStore()

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
	read, err := resources.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.VersionID != "1" {
		t.Fatalf("VersionID = %q, want 1", read.VersionID)
	}

	envelope.VersionID = "2"
	envelope.Hash = "hash-v2"
	if err := resources.Update(ctx, envelope); err != nil {
		t.Fatalf("Update: %v", err)
	}
	read, err = resources.Read(ctx, "Patient", "pat-1")
	if err != nil || read.VersionID != "2" {
		t.Fatalf("Read after update = %+v, %v", read, err)
	}

	if err := resources.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err := resources.Exists(ctx, "Patient", "pat-1")
	if err != nil || exists {
		t.Fatalf("Exists after delete = %v, %v", exists, err)
	}
}

func TestAuthStoreSnapshotHydratesEngine(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "auth")
	authStore := tdb.AuthStore()
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
		Tenant:       auth.TenantContext{TenantID: tdb.TenantID()},
		ResourceType: "Appointment",
	})
	if err != nil {
		t.Fatalf("CanReadResource: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed", decision)
	}
}

func TestHistoryStoreAppendAndGet(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "history")
	history := tdb.HistoryStore()

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
	last := versions[1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last entry = %+v, want delete tombstone", last)
	}
}

func TestEventStoreAppendAndReadSince(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "events")
	events := tdb.EventStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	e1, err := events.Append(ctx, store.ResourceEvent{
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

	e2, err := events.Append(ctx, store.ResourceEvent{
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

	all, err := events.ReadSince(ctx, 0, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("ReadSince all = %d, %v", len(all), err)
	}
}

func TestApplyWriteAtomic(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "write")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	result, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		SearchEntries: []store.SearchIndexEntry{{
			ResourceType: "Patient",
			ID:           "pat-1",
			Fields:       map[string]string{"string.family": "Doe"},
		}},
		Audit: store.AuditRecord{Actor: "test", Action: "create"},
	})
	if err != nil {
		t.Fatalf("ApplyWrite: %v", err)
	}
	if result.Outcome != postgres.WriteOutcomeAccepted {
		t.Fatalf("outcome = %q, want accepted", result.Outcome)
	}
	if result.Event.Sequence != 1 {
		t.Errorf("event sequence = %d, want 1", result.Event.Sequence)
	}
	if result.Resource.VersionID == "" {
		t.Error("expected assigned version ID")
	}
	if !result.IDRegistry.Registered {
		t.Error("expected ID registry registration")
	}

	exists, err := tdb.ResourceStore().Exists(ctx, "Patient", "pat-1")
	if err != nil || !exists {
		t.Fatalf("resource exists = %v, %v", exists, err)
	}
	history, err := tdb.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	ids, err := tdb.SearchStore().Lookup(ctx, "string.family", "Doe")
	if err != nil || len(ids) != 1 {
		t.Fatalf("search ids = %v, %v", ids, err)
	}
}

func TestApplyWriteRollbackOnError(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "rollback")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if _, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Audit:    store.AuditRecord{Actor: "test", Action: "create"},
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	_, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Audit:    store.AuditRecord{Actor: "test", Action: "create"},
	})
	if err == nil {
		t.Fatal("expected duplicate create to fail")
	}

	history, err := tdb.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events length = %d, want 1", len(events))
	}
}

func TestApplyWriteConflictedDoesNotMutateResource(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "conflict")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	accepted, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Audit:    store.AuditRecord{Actor: "test", Action: "create"},
	})
	if err != nil {
		t.Fatalf("accepted write: %v", err)
	}

	conflicted, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource:                envelope,
		Action:                  store.VersionActionUpdate,
		Outcome:                 postgres.WriteOutcomeConflicted,
		RejectionReason:         "version mismatch",
		ConflictLocalVersionID:  "local-v",
		ConflictRemoteVersionID: accepted.Resource.VersionID,
		Audit:                   store.AuditRecord{Actor: "test", Action: "update"},
	})
	if err != nil {
		t.Fatalf("conflicted write: %v", err)
	}
	if conflicted.Outcome != postgres.WriteOutcomeConflicted {
		t.Fatalf("outcome = %q", conflicted.Outcome)
	}

	read, err := tdb.ResourceStore().Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.VersionID != accepted.Resource.VersionID {
		t.Fatalf("resource mutated: got %q want %q", read.VersionID, accepted.Resource.VersionID)
	}

	history, err := tdb.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, want 1", len(history))
	}
	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	conflicts, err := tdb.ConflictStore().List(ctx, "Patient", "pat-1")
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("conflicts = %d, %v", len(conflicts), err)
	}

	records, err := tdb.AuditStore().List(ctx, store.AuditQuery{Limit: 10})
	if err != nil || len(records) < 2 {
		t.Fatalf("audit records = %d, %v", len(records), err)
	}
}

func TestIDRegistryPreventsDuplicateRegistration(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "idreg")
	reg := tdb.IDRegistry()

	if err := reg.Reserve(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := reg.Reserve(ctx, "Patient", "pat-1"); err == nil {
		t.Fatal("expected duplicate reserve to fail")
	}
}

func TestSearchStoreIndexLookupRemove(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "search")
	search := tdb.SearchStore()

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
	if err != nil || len(ids) != 1 || ids[0] != "pat-1" {
		t.Fatalf("Lookup string ids = %v, %v", ids, err)
	}

	if err := search.RemoveIndex(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("RemoveIndex: %v", err)
	}
	ids, err = search.Lookup(ctx, "string.family", "Doe")
	if err != nil || len(ids) != 0 {
		t.Fatalf("Lookup after remove = %v, %v", ids, err)
	}
}

func TestCursorStoreUpsertGetDelete(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "cursor")
	cursors := tdb.CursorStore()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	got, err := cursors.GetCursor(ctx, "sync-worker")
	if err != nil || got != nil {
		t.Fatalf("GetCursor missing = %+v, %v", got, err)
	}

	if err := cursors.UpsertCursor(ctx, store.Cursor{Name: "sync-worker", Position: "42", UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertCursor: %v", err)
	}
	got, err = cursors.GetCursor(ctx, "sync-worker")
	if err != nil || got == nil || got.Position != "42" {
		t.Fatalf("GetCursor = %+v, %v", got, err)
	}

	if err := cursors.DeleteCursor(ctx, "sync-worker"); err != nil {
		t.Fatalf("DeleteCursor: %v", err)
	}
}

func TestInboxStoreMarkApplied(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "inbox")
	inbox := tdb.InboxStore()
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

func TestBinaryBlobAuditModuleStores(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "aux")
	now := time.Now().UTC()

	if err := tdb.BinaryStore().Put(ctx, store.BinaryObject{
		Key: "bin-1", ContentType: "text/plain", Size: 3, Data: []byte("abc"), CreatedAt: now,
	}); err != nil {
		t.Fatalf("Binary Put: %v", err)
	}
	bin, err := tdb.BinaryStore().Get(ctx, "bin-1")
	if err != nil || string(bin.Data) != "abc" {
		t.Fatalf("Binary Get = %+v, %v", bin, err)
	}

	if err := tdb.BlobStore().Put(ctx, store.BlobObject{
		Key: "blob-1", Size: 10, Location: "s3://bucket/key", CreatedAt: now,
	}); err != nil {
		t.Fatalf("Blob Put: %v", err)
	}
	head, err := tdb.BlobStore().Head(ctx, "blob-1")
	if err != nil || head.Location != "s3://bucket/key" {
		t.Fatalf("Blob Head = %+v, %v", head, err)
	}

	auditID := uuid.NewString()
	if err := tdb.AuditStore().Append(ctx, store.AuditRecord{
		ID: auditID, Timestamp: now, Actor: "tester", Action: "read", Outcome: "ok",
	}); err != nil {
		t.Fatalf("Audit Append: %v", err)
	}
	records, err := tdb.AuditStore().List(ctx, store.AuditQuery{Actor: "tester", Limit: 5})
	if err != nil || len(records) != 1 {
		t.Fatalf("Audit List = %d, %v", len(records), err)
	}

	if err := tdb.ModuleStore().Register(ctx, store.ModuleRecord{
		Name: "search", Version: "1.0.0", RegisteredAt: now,
	}); err != nil {
		t.Fatalf("Module Register: %v", err)
	}
	mod, err := tdb.ModuleStore().Get(ctx, "search")
	if err != nil || mod.Version != "1.0.0" {
		t.Fatalf("Module Get = %+v, %v", mod, err)
	}
}

func TestConcurrentWriteSequencing(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "concurrent")

	var wg sync.WaitGroup
	sequences := make(chan int64, 20)
	errs := make(chan error, 20)

	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("pat-%d", i)
			now := time.Now().UTC()
			result, err := tdb.ApplyWrite(ctx, postgres.Write{
				Resource: &types.ResourceEnvelope{
					ResourceType: "Patient",
					ID:           id,
					LastUpdated:  now,
					JSON:         []byte(`{"resourceType":"Patient","id":"` + id + `"}`),
					Hash:         "hash",
				},
				Action: store.VersionActionCreate,
				Audit:  store.AuditRecord{Actor: "test", Action: "create"},
			})
			if err != nil {
				errs <- err
				return
			}
			sequences <- result.Event.Sequence
		}(i)
	}

	wg.Wait()
	close(errs)
	close(sequences)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write error: %v", err)
		}
	}

	seen := map[int64]bool{}
	for seq := range sequences {
		if seen[seq] {
			t.Fatalf("duplicate sequence %d", seq)
		}
		seen[seq] = true
	}
	if len(seen) != 10 {
		t.Fatalf("got %d unique sequences, want 10", len(seen))
	}
}

func TestNodeRegistryRegisterGetListUnregister(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "nodes")
	nodes := tdb.NodeRegistry()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := nodes.Register(ctx, store.NodeRecord{
		NodeID:       "edge-1",
		Metadata:     map[string]string{"region": "us-east"},
		RegisteredAt: now,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := nodes.Get(ctx, "edge-1")
	if err != nil || got.Metadata["region"] != "us-east" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	list, err := nodes.List(ctx)
	if err != nil || len(list) != 1 || list[0].NodeID != "edge-1" {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if err := nodes.Unregister(ctx, "edge-1"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	list, err = nodes.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("List after unregister = %+v, %v", list, err)
	}
}

func TestJobStoreEnqueueClaimUpdate(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "jobs")
	jobs := tdb.JobStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := jobs.Enqueue(ctx, store.JobRecord{
		ID:        "job-1",
		Type:      "reindex",
		Payload:   []byte(`{"view":"patient-summary"}`),
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := jobs.ClaimNext(ctx, "reindex")
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNext = %+v, %v", claimed, err)
	}
	if claimed.ID != "job-1" || claimed.Status != store.JobStatusRunning {
		t.Fatalf("ClaimNext = %+v", claimed)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", claimed.Attempts)
	}

	claimed.Status = store.JobStatusCompleted
	claimed.UpdatedAt = now.Add(time.Minute)
	if err := jobs.Update(ctx, *claimed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := jobs.Get(ctx, "job-1")
	if err != nil || got.Status != store.JobStatusCompleted {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestMaterializedViewStoreUpsertGetDeleteListKeys(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "views")
	views := tdb.MaterializedViewStore()
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	if err := views.Upsert(ctx, store.MaterializedViewRecord{
		ViewName:  "patient-summary",
		Key:       "pat-1",
		Payload:   []byte(`{"count":1}`),
		Version:   1,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := views.Get(ctx, "patient-summary", "pat-1")
	if err != nil || string(got.Payload) != `{"count":1}` {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	keys, err := views.ListKeys(ctx, "patient-summary")
	if err != nil || len(keys) != 1 || keys[0] != "pat-1" {
		t.Fatalf("ListKeys = %v, %v", keys, err)
	}

	if err := views.Delete(ctx, "patient-summary", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	keys, err = views.ListKeys(ctx, "patient-summary")
	if err != nil || len(keys) != 0 {
		t.Fatalf("ListKeys after delete = %v, %v", keys, err)
	}
}

func TestAnalyticsStoreAppendQueryPrepared(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "analytics")
	analytics := tdb.AnalyticsStore()
	early := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)

	if err := analytics.Append(ctx, store.AnalyticsEvent{
		ID:         "evt-1",
		Name:       "page.view",
		Timestamp:  early,
		Dimensions: map[string]string{"page": "home"},
		Values:     map[string]float64{"count": 1},
	}); err != nil {
		t.Fatalf("Append early: %v", err)
	}
	if err := analytics.Append(ctx, store.AnalyticsEvent{
		ID:        "evt-2",
		Name:      "page.view",
		Timestamp: late,
	}); err != nil {
		t.Fatalf("Append late: %v", err)
	}

	events, err := analytics.QueryPrepared(ctx, store.PreparedQuery{Name: "by-name-since"}, map[string]string{
		"name":  "page.view",
		"since": early.Add(30 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("QueryPrepared: %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt-2" {
		t.Fatalf("QueryPrepared = %+v", events)
	}
}

func TestApplyWriteRejectedDoesNotMutateResource(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "rejected")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	accepted, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		Audit:    store.AuditRecord{Actor: "test", Action: "create"},
	})
	if err != nil {
		t.Fatalf("accepted write: %v", err)
	}

	rejected, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource:        envelope,
		Action:          store.VersionActionUpdate,
		Outcome:         postgres.WriteOutcomeRejected,
		RejectionReason: "invalid payload",
		Audit:           store.AuditRecord{Actor: "test", Action: "update"},
	})
	if err != nil {
		t.Fatalf("rejected write: %v", err)
	}
	if rejected.Outcome != postgres.WriteOutcomeRejected {
		t.Fatalf("outcome = %q", rejected.Outcome)
	}

	read, err := tdb.ResourceStore().Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.VersionID != accepted.Resource.VersionID {
		t.Fatalf("resource mutated: got %q want %q", read.VersionID, accepted.Resource.VersionID)
	}

	history, err := tdb.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, want 1", len(history))
	}
	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	conflicts, err := tdb.ConflictStore().List(ctx, "Patient", "pat-1")
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("conflicts = %d, want 0", len(conflicts))
	}

	records, err := tdb.AuditStore().List(ctx, store.AuditQuery{Limit: 10})
	if err != nil || len(records) < 2 {
		t.Fatalf("audit records = %d, %v", len(records), err)
	}
	last := records[len(records)-1]
	if last.Outcome != string(postgres.WriteOutcomeRejected) {
		t.Fatalf("last audit outcome = %q", last.Outcome)
	}
}

func TestApplyWriteDelete(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "delete")
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	envelope := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "pat-1",
		LastUpdated:  now,
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
		Hash:         "hash-v1",
	}

	if _, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionCreate,
		SearchEntries: []store.SearchIndexEntry{{
			ResourceType: "Patient",
			ID:           "pat-1",
			Fields:       map[string]string{"string.family": "Doe"},
		}},
		Audit: store.AuditRecord{Actor: "test", Action: "create"},
	}); err != nil {
		t.Fatalf("create write: %v", err)
	}

	_, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: envelope,
		Action:   store.VersionActionDelete,
		Audit:    store.AuditRecord{Actor: "test", Action: "delete"},
	})
	if err != nil {
		t.Fatalf("delete write: %v", err)
	}

	exists, err := tdb.ResourceStore().Exists(ctx, "Patient", "pat-1")
	if err != nil || exists {
		t.Fatalf("resource should be deleted: exists=%v err=%v", exists, err)
	}
	history, err := tdb.HistoryStore().GetHistory(ctx, "Patient", "pat-1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	last := history[len(history)-1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last history = %+v", last)
	}
	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	ids, err := tdb.SearchStore().Lookup(ctx, "string.family", "Doe")
	if err != nil || len(ids) != 0 {
		t.Fatalf("search should be cleared: %v, %v", ids, err)
	}
}

func TestConflictStoreResolve(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx := context.Background()
	tdb := testTenant(t, db, "conflict-resolve")
	conflicts := tdb.ConflictStore()
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
	if err != nil || len(list) != 1 || list[0].ID != "conf-1" {
		t.Fatalf("List = %+v, %v", list, err)
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
