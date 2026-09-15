package golden_test

import (
	"fmt"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/testkit/golden"
)

func TestAuthOutcomeCatalogMatchesStableDiagnostics(t *testing.T) {
	cases := []struct {
		key string
		err error
	}{
		{"security_token_expired", fmt.Errorf("%w: exp 2026-07-14T11:00:00Z", smart.ErrTokenExpired)},
		{"security_token_not_yet_valid", fmt.Errorf("%w: nbf 2026-07-14T13:00:00Z", smart.ErrTokenNotYetValid)},
		{"security_replay", smart.ErrReplay},
	}
	for _, tc := range cases {
		got := golden.SecurityOutcome(smart.StableAuthDiagnostics(tc.err))
		golden.AssertOutcomeEqual(t, got, golden.AuthOutcomeCatalog[tc.key])
	}
}
