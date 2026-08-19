package terminology_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/terminology"
)

func TestNewLocalService(t *testing.T) {
	svc := terminology.NewLocalService(nil, "tenant-a", terminology.WithMaxExpansion(100))
	if svc.ScopeID != "tenant-a" {
		t.Fatalf("ScopeID = %q", svc.ScopeID)
	}
	if svc.MaxExpansion != 100 {
		t.Fatalf("MaxExpansion = %d", svc.MaxExpansion)
	}
}
