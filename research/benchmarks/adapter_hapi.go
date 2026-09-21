package benchmarks

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// HAPIAdapter is a template for an external FHIR server. It is not used in CI.
// Implement Load/Read with FHIR REST (PUT/GET) and RunView with a
// ViewDefinition $run operation when the server supports SQL-on-FHIR.
type HAPIAdapter struct {
	BaseURL string
}

// Name implements Adapter.
func (a *HAPIAdapter) Name() string { return "hapi-template" }

// Load documents the expected ingest path.
func (a *HAPIAdapter) Load(context.Context, []*types.ResourceEnvelope) error {
	return fmt.Errorf("hapi adapter template: implement PUT {base}/{type}/{id} (not executed in CI)")
}

// Read documents the expected read path.
func (a *HAPIAdapter) Read(context.Context, string, string) error {
	return fmt.Errorf("hapi adapter template: implement GET {base}/{type}/{id}")
}

// RunView documents the expected view path.
func (a *HAPIAdapter) RunView(context.Context, string, string) (int, error) {
	return 0, fmt.Errorf("hapi adapter template: implement ViewDefinition/$run when available")
}

// RunAIView is HAIStack-specific and should be omitted for HAPI comparisons.
func (a *HAPIAdapter) RunAIView(context.Context, string, string) (int, error) {
	return 0, fmt.Errorf("hapi adapter template: omit ai-run-view (optional workload)")
}
