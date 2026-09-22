package validate

import (
	"context"

	"github.com/degoke/haistack/pkg/types"
)

// Validator checks a resource envelope before lifecycle mutations in pkg/core.
type Validator interface {
	// ValidateResource returns nil when resource is acceptable for persistence.
	ValidateResource(ctx context.Context, resource *types.ResourceEnvelope) error
}
