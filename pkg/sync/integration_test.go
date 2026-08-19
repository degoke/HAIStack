package sync_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSQLiteDevicePostgresHubIntegration(t *testing.T) {
	ctx := context.Background()
	pdb, cleanup := openIntegrationPostgres(t)
	defer cleanup()

	tenantID := fmt.Sprintf("tenant-sync-%d", time.Now().UnixNano())
	if err := pdb.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := pdb.Tenant(tenantID)
	hub := &hasync.PostgresHub{Tenant: tdb}

	device, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })
	if err := device.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	now := time.Date(2026, 7, 6, 16, 0, 0, 0, time.UTC)
	res := sampleResource("p-sync", "local-v1")
	if _, err := device.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: res,
		Action:   store.VersionActionCreate,
		Version: store.ResourceVersion{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     res,
			Hash:         res.Hash,
		},
		Event: store.ResourceEvent{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.EventActionCreate,
			Timestamp:    now,
			Hash:         res.Hash,
		},
	}); err != nil {
		t.Fatalf("local write: %v", err)
	}

	engine := hasync.NewEngine(hasync.Config{
		NodeID:    "device-1",
		TenantID:  tenantID,
		Events:    device.OutboxStore(),
		Cursors:   device.CursorStore(),
		Inbox:     device.InboxStore(),
		Resources: device.ResourceStore(),
		History:   device.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	})

	push, err := engine.Push(ctx)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if push.Results[0].State != hasync.AckAccepted {
		t.Fatalf("push result = %+v", push.Results[0])
	}

	replica, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open replica: %v", err)
	}
	t.Cleanup(func() { _ = replica.Close() })
	if err := replica.Migrate(ctx); err != nil {
		t.Fatalf("migrate replica: %v", err)
	}

	pull, err := hasync.NewEngine(hasync.Config{
		NodeID:    "device-2",
		TenantID:  tenantID,
		Cursors:   replica.CursorStore(),
		Inbox:     replica.InboxStore(),
		Resources: replica.ResourceStore(),
		History:   replica.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	}).Pull(ctx)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if pull.Applied != 1 {
		t.Fatalf("pull = %+v", pull)
	}

	exists, err := replica.ResourceStore().Exists(ctx, "Patient", "p-sync")
	if err != nil || !exists {
		t.Fatalf("replica resource missing: exists=%v err=%v", exists, err)
	}

	events, err := tdb.EventStore().ReadSince(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("hub events = %d err=%v", len(events), err)
	}
}

func TestPostgresHubPullUsesExactHistoricalPayload(t *testing.T) {
	ctx := context.Background()
	pdb, cleanup := openIntegrationPostgres(t)
	defer cleanup()

	tenantID := fmt.Sprintf("tenant-history-%d", time.Now().UnixNano())
	if err := pdb.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	tdb := pdb.Tenant(tenantID)

	first := sampleResource("p-history", "local-v1")
	first.JSON = []byte(`{"resourceType":"Patient","id":"p-history","gender":"male"}`)
	first.Hash = "history-v1"
	created, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource: first,
		Action:   store.VersionActionCreate,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	second := sampleResource("p-history", "local-v2")
	second.JSON = []byte(`{"resourceType":"Patient","id":"p-history","gender":"female"}`)
	second.Hash = "history-v2"
	updated, err := tdb.ApplyWrite(ctx, postgres.Write{
		Resource:        second,
		Action:          store.VersionActionUpdate,
		ExpectedVersion: created.Version.VersionID,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	events, err := (&hasync.PostgresHub{Tenant: tdb}).Pull(ctx, 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("canonical events = %d, want 2", len(events))
	}
	if events[0].CanonicalVersionID != created.Version.VersionID || events[1].CanonicalVersionID != updated.Version.VersionID {
		t.Fatalf("canonical versions = %q, %q", events[0].CanonicalVersionID, events[1].CanonicalVersionID)
	}
	if string(events[0].ResourceAfter.JSON) != string(first.JSON) {
		t.Fatalf("first payload = %s, want historical v1 payload %s", events[0].ResourceAfter.JSON, first.JSON)
	}
	if string(events[1].ResourceAfter.JSON) != string(second.JSON) {
		t.Fatalf("second payload = %s, want historical v2 payload %s", events[1].ResourceAfter.JSON, second.JSON)
	}
}

func TestPostgresHubDedupeReturnsOriginalAcknowledgement(t *testing.T) {
	ctx := context.Background()
	pdb, cleanup := openIntegrationPostgres(t)
	defer cleanup()

	tenantID := fmt.Sprintf("tenant-ack-%d", time.Now().UnixNano())
	if err := pdb.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	hub := &hasync.PostgresHub{Tenant: pdb.Tenant(tenantID)}

	first := sampleResource("p-ack", "local-v1")
	firstEvent := hasync.LocalEvent{
		EventID:       "event-ack-1",
		OriginNodeID:  "node-a",
		TenantID:      tenantID,
		ResourceType:  first.ResourceType,
		ResourceID:    first.ID,
		Operation:     hasync.EventTypeResourceCreated,
		LocalVersion:  first.VersionID,
		ResourceAfter: first,
		ResourceHash:  first.Hash,
	}
	firstAck, err := hub.Push(ctx, []hasync.LocalEvent{firstEvent})
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	if len(firstAck) != 1 || firstAck[0].State != hasync.AckAccepted {
		t.Fatalf("first ack = %+v", firstAck)
	}

	second := sampleResource("p-ack", "local-v2")
	secondEvent := firstEvent
	secondEvent.EventID = "event-ack-2"
	secondEvent.Operation = hasync.EventTypeResourceUpdated
	secondEvent.LocalVersion = second.VersionID
	secondEvent.BaseCloudVersion = firstAck[0].CanonicalVersionID
	secondEvent.ResourceAfter = second
	secondAck, err := hub.Push(ctx, []hasync.LocalEvent{secondEvent})
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if len(secondAck) != 1 || secondAck[0].State != hasync.AckAccepted {
		t.Fatalf("second ack = %+v", secondAck)
	}
	if secondAck[0].CanonicalSequence == firstAck[0].CanonicalSequence {
		t.Fatal("second write reused first canonical sequence")
	}

	replayed, err := hub.Push(ctx, []hasync.LocalEvent{firstEvent})
	if err != nil {
		t.Fatalf("replayed push: %v", err)
	}
	if len(replayed) != 1 || replayed[0].State != hasync.AckAlreadyProcessed {
		t.Fatalf("replayed ack = %+v", replayed)
	}
	if replayed[0].CanonicalSequence != firstAck[0].CanonicalSequence || replayed[0].CanonicalVersionID != firstAck[0].CanonicalVersionID {
		t.Fatalf("replayed ack = %+v, want original %+v", replayed[0], firstAck[0])
	}
}

func TestPostgresHubConcurrentFirstPushClaimsEventOnce(t *testing.T) {
	ctx := context.Background()
	pdb, cleanup := openIntegrationPostgres(t)
	defer cleanup()

	tenantID := fmt.Sprintf("tenant-concurrent-ack-%d", time.Now().UnixNano())
	if err := pdb.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	event := hasync.LocalEvent{
		EventID:       "event-concurrent-1",
		OriginNodeID:  "node-a",
		TenantID:      tenantID,
		ResourceType:  "Patient",
		ResourceID:    "p-concurrent",
		Operation:     hasync.EventTypeResourceCreated,
		LocalVersion:  "local-v1",
		ResourceAfter: sampleResource("p-concurrent", "local-v1"),
	}

	hub := &hasync.PostgresHub{Tenant: pdb.Tenant(tenantID)}
	results := make(chan []hasync.PushResult, 2)
	errs := make(chan error, 2)
	var wg stdsync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ack, err := hub.Push(ctx, []hasync.LocalEvent{event})
			results <- ack
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	var states []hasync.AckState
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent push: %v", err)
		}
	}
	for ack := range results {
		if len(ack) != 1 {
			t.Fatalf("concurrent ack = %+v", ack)
		}
		states = append(states, ack[0].State)
	}
	if len(states) != 2 {
		t.Fatalf("received %d concurrent acknowledgements", len(states))
	}
	accepted, replayed := 0, 0
	for _, state := range states {
		switch state {
		case hasync.AckAccepted:
			accepted++
		case hasync.AckAlreadyProcessed:
			replayed++
		}
	}
	if accepted != 1 || replayed != 1 {
		t.Fatalf("concurrent states = %v, want one accepted and one already_processed", states)
	}
	events, err := pdb.Tenant(tenantID).EventStore().ReadSince(ctx, 0, 10)
	if err != nil {
		t.Fatalf("read canonical events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("canonical event count = %d, want 1", len(events))
	}
}

func openIntegrationPostgres(t *testing.T) (*postgres.DB, func()) {
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

	if !integrationDockerAvailable() {
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
		t.Skipf("postgres unavailable: %v", err)
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

	return db, func() {
		db.Close()
		_ = container.Terminate(ctx)
	}
}

func integrationDockerAvailable() bool {
	if os.Getenv("DOCKER_HOST") == "" {
		out, err := exec.Command("docker", "context", "inspect", "-f", "{{.Endpoints.docker.Host}}").Output()
		if err == nil {
			if host := strings.TrimSpace(string(out)); host != "" {
				_ = os.Setenv("DOCKER_HOST", host)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}
