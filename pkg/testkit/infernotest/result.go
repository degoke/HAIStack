package infernotest

import (
	"fmt"
	"strings"
)

// OK reports whether discovery checks passed.
func (r DiscoveryResult) OK() bool {
	return len(r.Errors) == 0 && r.StatusCode == 200
}

// Errorf returns a formatted multi-line error for test failures.
func (r DiscoveryResult) Errorf() error {
	if r.OK() {
		return nil
	}
	return fmt.Errorf("inferno discovery failed for %s:\n  %s", r.WellKnownURL, strings.Join(r.Errors, "\n  "))
}
