package audit_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestStoreAdapterRoundTrip(t *testing.T) {
	ctx := context.Background()
	mem := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{
		Store: mem,
		Now:   func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) },
		NewID: func() string { return "fixed-id" },
	}

	if err := audit.LogAIToolCall(ctx, logger, audit.AIToolCallEvent{
		Actor:          "agent-1",
		Tenant:         "tenant-a",
		Subject:        "patient/pat-1",
		ToolName:       "read_fhir_resource",
		Outcome:        audit.OutcomeSuccess,
		ConversationID: "c1",
		Details:        map[string]string{"resourceType": "Patient"},
	}); err != nil {
		t.Fatalf("LogAIToolCall: %v", err)
	}

	events, err := logger.ListEvents(ctx, audit.Query{Actor: "agent-1", Action: audit.ActionExecuteTool})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	ev := events[0]
	if ev.ID != "fixed-id" || ev.ToolName != "read_fhir_resource" || ev.Tenant != "tenant-a" {
		t.Fatalf("event = %#v", ev)
	}
	if ev.Details["conversationId"] != "c1" || ev.Details["resourceType"] != "Patient" {
		t.Fatalf("details = %#v", ev.Details)
	}
}

func TestEmitHelpersActionNames(t *testing.T) {
	ctx := context.Background()
	mem := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: mem}

	cases := []struct {
		name   string
		emit   func() error
		action string
	}{
		{"read", func() error {
			return audit.LogResourceRead(ctx, logger, audit.ResourceReadEvent{Actor: "a", ResourceType: "Patient", ResourceID: "1"})
		}, audit.ActionResourceRead},
		{"write", func() error {
			return audit.LogResourceWrite(ctx, logger, audit.ResourceWriteEvent{Actor: "a", ResourceType: "Patient", ResourceID: "1", Operation: "create"})
		}, audit.ActionResourceWrite},
		{"sync", func() error {
			return audit.LogSyncEvent(ctx, logger, audit.SyncEvent{Actor: "device", Action: audit.ActionSyncAccepted, Outcome: "ok"})
		}, audit.ActionSyncAccepted},
		{"auth-allow", func() error {
			return audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{Actor: "u", Allowed: true, Reason: "ok"})
		}, audit.ActionAuthAllow},
		{"auth-deny", func() error {
			return audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{Actor: "u", Allowed: false, Reason: "no"})
		}, audit.ActionAuthDeny},
		{"export", func() error {
			return audit.LogExport(ctx, logger, audit.ExportEvent{Actor: "u", ViewName: "v1"})
		}, audit.ActionExport},
		{"blob", func() error {
			return audit.LogBlobAccess(ctx, logger, audit.BlobAccessEvent{Actor: "u", BlobKey: "k1"})
		}, audit.ActionBlobAccess},
		{"view", func() error {
			return audit.LogViewAccess(ctx, logger, audit.ViewAccessEvent{Actor: "u", ViewName: "summary", Version: "1", Outcome: audit.OutcomeSuccess})
		}, audit.ActionExecuteView},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(mem.Records())
			if err := tc.emit(); err != nil {
				t.Fatalf("emit: %v", err)
			}
			recs := mem.Records()
			if len(recs) != before+1 {
				t.Fatalf("records = %d", len(recs))
			}
			if recs[len(recs)-1].Action != tc.action {
				t.Fatalf("action = %q, want %q", recs[len(recs)-1].Action, tc.action)
			}
		})
	}
}

func TestToFromStoreRecord(t *testing.T) {
	ev := audit.Event{
		ID:           "id-1",
		Timestamp:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Actor:        "actor",
		Tenant:       "t1",
		Subject:      "sub",
		Action:       audit.ActionExecuteView,
		Outcome:      audit.OutcomeSuccess,
		ResourceType: "Patient",
		ResourceID:   "p1",
		ViewName:     "summary",
		ToolName:     "run_view",
		ModuleName:   "scheduling",
		BlobKey:      "blob-1",
		Details:      map[string]string{"extra": "x"},
	}
	rec := audit.ToStoreRecord(ev)
	back := audit.FromStoreRecord(rec)
	if back.Tenant != "t1" || back.ViewName != "summary" || back.ToolName != "run_view" {
		t.Fatalf("round trip = %#v", back)
	}
	if back.Details["extra"] != "x" {
		t.Fatalf("details = %#v", back.Details)
	}
	if _, ok := back.Details["tenant"]; ok {
		t.Fatal("tenant should be lifted out of details")
	}
}

func TestSQLiteAuditAppendList(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	storeAdapter := &audit.StoreAdapter{Store: db.AuditStore()}

	now := time.Now().UTC()
	if err := audit.LogResourceRead(ctx, storeAdapter, audit.ResourceReadEvent{
		ID:           "audit-sqlite-1",
		Actor:        "tester",
		ResourceType: "Patient",
		ResourceID:   "pat-1",
		Timestamp:    now,
	}); err != nil {
		t.Fatalf("LogResourceRead: %v", err)
	}

	recs, err := db.AuditStore().List(ctx, store.AuditQuery{Actor: "tester", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].Action != audit.ActionResourceRead {
		t.Fatalf("recs = %#v", recs)
	}
}

func TestMemoryAndSQLiteListFilterParity(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	mem := audit.NewMemoryStore()
	sqlDB := openSQLite(t)

	backends := []struct {
		name  string
		store store.AuditStore
	}{
		{"memory", mem},
		{"sqlite", sqlDB.AuditStore()},
	}

	for _, tc := range backends {
		t.Run(tc.name, func(t *testing.T) {
			logger := &audit.StoreAdapter{Store: tc.store}
			_ = audit.LogResourceRead(ctx, logger, audit.ResourceReadEvent{
				ID: "parity-a-" + tc.name, Actor: "alice", ResourceType: "Patient", ResourceID: "1", Timestamp: now,
			})
			_ = audit.LogResourceRead(ctx, logger, audit.ResourceReadEvent{
				ID: "parity-b-" + tc.name, Actor: "bob", ResourceType: "Observation", ResourceID: "2", Timestamp: now.Add(time.Second),
			})

			got, err := tc.store.List(ctx, store.AuditQuery{Actor: "alice", Limit: 10})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != 1 || got[0].Actor != "alice" {
				t.Fatalf("got = %#v", got)
			}
			got, err = tc.store.List(ctx, store.AuditQuery{ResourceType: "Observation", Limit: 10})
			if err != nil || len(got) != 1 {
				t.Fatalf("by type: %#v err=%v", got, err)
			}
		})
	}
}

func TestEmitRequiresLogger(t *testing.T) {
	err := audit.LogResourceRead(context.Background(), nil, audit.ResourceReadEvent{
		Actor:        "tester",
		ResourceType: "Patient",
		ResourceID:   "1",
	})
	if !errors.Is(err, audit.ErrNilLogger) {
		t.Fatalf("err = %v, want ErrNilLogger", err)
	}
}

func TestStoreAdapterRequiresStore(t *testing.T) {
	logger := &audit.StoreAdapter{}
	err := logger.Log(context.Background(), audit.Event{
		ID:        "audit-1",
		Timestamp: time.Now().UTC(),
		Actor:     "tester",
		Action:    audit.ActionResourceRead,
	})
	if !errors.Is(err, audit.ErrNilStore) {
		t.Fatalf("err = %v, want ErrNilStore", err)
	}
}

func openSQLite(t *testing.T) *sqlite.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.db")
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
