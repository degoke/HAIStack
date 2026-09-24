package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

func TestSessionServiceCreateAppendGet(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, tempDBPath(t))
	svc := db.SessionService("tenant-a")

	created, err := svc.CreateSession(ctx, store.CreateSessionParams{
		TenantID:     "tenant-a",
		AppName:      "demo-app",
		UserID:       "user-1",
		Subject:      "patient/p1",
		InitialState: map[string]any{"locale": "en"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, err = svc.AppendEvent(ctx, store.AppendEventParams{
		TenantID: "tenant-a", AppName: "demo-app", UserID: "user-1", SessionID: created.ID,
		Event: store.SessionEvent{
			Author: store.SessionAuthorUser, Content: "hello", Timestamp: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	got, err := svc.GetSession(ctx, store.GetSessionParams{
		TenantID: "tenant-a", AppName: "demo-app", UserID: "user-1", SessionID: created.ID,
	})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Content != "hello" {
		t.Fatalf("events = %+v", got.Events)
	}
	if got.State["locale"] != "en" {
		t.Fatalf("state = %v", got.State)
	}

	if err := svc.DeleteSession(ctx, store.DeleteSessionParams{
		TenantID: "tenant-a", AppName: "demo-app", UserID: "user-1", SessionID: created.ID,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetSession(ctx, store.GetSessionParams{
		TenantID: "tenant-a", AppName: "demo-app", UserID: "user-1", SessionID: created.ID,
	})
	if !errors.Is(err, store.ErrSessionNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
