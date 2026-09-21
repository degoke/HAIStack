package search

import (
	"errors"
	"fmt"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type kindedError struct {
	kind string
	msg  string
}

func (e kindedError) Error() string { return e.msg }
func (e kindedError) Kind() string  { return e.kind }

func TestIsResourceNotFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "store sentinel", err: store.ErrNotFound, want: true},
		{name: "wrapped store sentinel", err: fmt.Errorf("%w: Patient/p1", store.ErrNotFound), want: true},
		{name: "kinded not-found", err: kindedError{kind: "not-found", msg: "missing"}, want: true},
		{name: "kinded other", err: kindedError{kind: "exception", msg: "not found in catalog"}, want: false},
		{name: "substring only", err: errors.New("search index not found in catalog"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isResourceNotFound(tt.err); got != tt.want {
				t.Fatalf("isResourceNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
