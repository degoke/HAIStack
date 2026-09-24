package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/store"
)

func TestConversationStorePutGetListDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, tempDBPath(t))
	conv := db.ConversationStore("tenant-a")

	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	record := store.ConversationRecord{
		ID:       "conv-1",
		TenantID: "tenant-a",
		Actor:    "agent-1",
		Messages: []store.ConversationMessage{
			{Role: "user", Content: "hello"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := conv.Put(ctx, record); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := conv.Get(ctx, "conv-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
		t.Fatalf("Get = %+v", got)
	}

	list, err := conv.List(ctx, store.ConversationQuery{Actor: "agent-1", Limit: 5})
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v err=%v", list, err)
	}

	if err := conv.Delete(ctx, "conv-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = conv.Get(ctx, "conv-1")
	if !errors.Is(err, store.ErrConversationNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}
