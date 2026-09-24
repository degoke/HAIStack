package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

func TestSessionServiceListEventsAfter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, tempDBPath(t))
	svc := db.SessionService("tenant-a")
	created, err := svc.CreateSession(ctx, store.CreateSessionParams{
		TenantID: "tenant-a", AppName: "app", UserID: "u", SessionID: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, content := range []string{"a", "b", "c"} {
		ev, err := svc.AppendEvent(ctx, store.AppendEventParams{
			TenantID: "tenant-a", AppName: "app", UserID: "u", SessionID: created.ID,
			Event: store.SessionEvent{
				ID:        "ev-" + string(rune('0'+i)),
				Author:    store.SessionAuthorUser,
				Content:   content,
				Timestamp: time.Now().UTC(),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ev.ID)
	}
	tail, err := svc.ListEventsAfter(ctx, store.ListEventsAfterParams{
		TenantID: "tenant-a", AppName: "app", UserID: "u", SessionID: created.ID,
		AfterEventID: ids[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].Content != "b" {
		t.Fatalf("tail = %+v", tail)
	}
}
