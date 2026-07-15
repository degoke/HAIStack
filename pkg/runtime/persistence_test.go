package runtime_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestPersistenceAccessorSQLite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rt, err := runtime.New().
		WithSQLite(t.TempDir()+"/persistence.db").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	p := rt.Persistence()
	if p.SQLite == nil {
		t.Fatal("expected sqlite db")
	}
	if p.Postgres != nil || p.TenantDB != nil {
		t.Fatal("expected postgres handles to be nil in sqlite mode")
	}
}
