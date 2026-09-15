package authztest

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// ExpectAllow and ExpectDeny are the two authorization outcomes scenarios assert.
const (
	ExpectAllow = true
	ExpectDeny  = false
)

// Scenario documents one authorization outcome test.
type Scenario struct {
	// Name is a stable identifier used in test output and CI matrices.
	Name string
	// Doc explains the principal, action, resource, and expected outcome.
	Doc string
	// Run executes the scenario against a Kit and returns an error on failure.
	Run func(ctx context.Context, kit *Kit) error
}

// Kit holds shared fixtures for running authorization scenarios.
type Kit struct {
	Engine  *auth.Engine
	Adapter *smart.AuthAdapter
}

// NewDefaultKit returns a Kit backed by the given engine and a default SMART adapter.
func NewDefaultKit(eng *auth.Engine) *Kit {
	return &Kit{Engine: eng, Adapter: SmartAdapter()}
}

// AssertDecision compares an authorization decision to the expected outcome.
func AssertDecision(name string, expectAllow bool, decision auth.Decision, err error) error {
	if err != nil {
		return fmt.Errorf("%s: unexpected error: %w", name, err)
	}
	if expectAllow && !decision.Allowed {
		return fmt.Errorf("%s: expected allow, got deny (%s)", name, decision.Reason)
	}
	if !expectAllow && decision.Allowed {
		return fmt.Errorf("%s: expected deny, got allow (%s)", name, decision.Reason)
	}
	return nil
}

// AssertDeniedError reports when an error was expected but not returned.
func AssertDeniedError(name string, err error) error {
	if err == nil {
		return fmt.Errorf("%s: expected error, got nil", name)
	}
	return nil
}
