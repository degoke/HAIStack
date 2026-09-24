package store_test

import (
	"testing"

	"github.com/degoke/haistack/pkg/store"
)

func TestActiveContextViewUsesLastCoveredEventID(t *testing.T) {
	events := []store.SessionEvent{
		{ID: "1", Author: store.SessionAuthorUser, Content: "old"},
		{ID: "2", Author: store.SessionAuthorModel, Content: "old reply"},
		{ID: "3", Author: store.SessionAuthorUser, Content: "tail user"},
		{ID: "4", Author: store.SessionAuthorCompaction, Content: "summary", Metadata: map[string]string{
			store.SessionMetadataLastCoveredEventID: "2",
		}},
	}
	summary, active := store.ActiveContextView(events)
	if summary != "summary" || len(active) != 1 || active[0].ID != "3" {
		t.Fatalf("summary=%q active=%+v", summary, active)
	}
}
