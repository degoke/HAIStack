package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestIsMissing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrJobNotFound, true},
		{fmt.Errorf("job not found: abc"), true},
		{fmt.Errorf("get job: job not found: abc"), true},
		{fmt.Errorf("connection refused"), false},
		{fmt.Errorf("table not found"), false},
		{fmt.Errorf("blob not found: x"), false},
		{fmt.Errorf("%w", ErrJobNotFound), true},
	}
	for _, tc := range cases {
		if got := IsMissing(tc.err); got != tc.want {
			t.Errorf("IsMissing(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

type errGetStore struct {
	store.JobStore
	err error
}

func (s errGetStore) Get(context.Context, string) (*store.JobRecord, error) {
	return nil, s.err
}

func TestGetRecordDistinguishesMissingFromFailure(t *testing.T) {
	ctx := context.Background()
	db := NewInMemoryJobStore()
	missing, err := GetRecord(ctx, db, TypeExportBulkRecord, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing = %#v %v", missing, err)
	}

	failed, err := GetRecord(ctx, errGetStore{JobStore: db, err: errors.New("connection refused")}, TypeExportBulkRecord, "x")
	if err == nil || failed != nil {
		t.Fatalf("failure GetRecord = %#v %v", failed, err)
	}
	if IsMissing(err) {
		t.Fatalf("connection error treated as missing: %v", err)
	}

	record, err := NewJob(TypeExportBulkRecord, map[string]string{"id": "job-1"}, EnqueueOptions{ID: "job-1"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := db.Enqueue(ctx, record); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := Lookup[map[string]string](ctx, db, TypeExportBulkRecord, "job-1")
	if err != nil || got == nil || (*got)["id"] != "job-1" {
		t.Fatalf("Lookup = %#v %v", got, err)
	}
	wrongType, err := GetRecord(ctx, db, TypeExportBulk, "job-1")
	if err != nil || wrongType != nil {
		t.Fatalf("type mismatch = %#v %v", wrongType, err)
	}
}
