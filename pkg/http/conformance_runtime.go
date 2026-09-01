package http

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// ConformanceRuntime is the hot-reloadable registry + validation view used by HTTP.
type ConformanceRuntime interface {
	Snapshot() *registry.Snapshot
	Engine() validate.Engine
	Refresh(ctx context.Context) (*registry.Snapshot, error)
}

// LiveCapabilitySource serves CapabilityStatement metadata from a hot-reloadable
// conformance runtime.
type LiveCapabilitySource struct {
	Runtime ConformanceRuntime
}

// LivePatientSearchParamResolver resolves patient search scope from the current
// conformance snapshot on each request.
type LivePatientSearchParamResolver struct {
	Runtime ConformanceRuntime
}

// PatientSearchParameterCode implements auth.PatientSearchParamResolver.
func (r LivePatientSearchParamResolver) PatientSearchParameterCode(resourceType string) (string, bool) {
	if r.Runtime == nil {
		return "", false
	}
	snapshot := r.Runtime.Snapshot()
	if snapshot == nil {
		return "", false
	}
	return snapshot.PatientSearchParameterCode(resourceType)
}

// CapabilitySnapshot implements CapabilitySource.
func (s LiveCapabilitySource) CapabilitySnapshot() registry.CapabilitySnapshot {
	if s.Runtime == nil || s.Runtime.Snapshot() == nil {
		return registry.CapabilitySnapshot{}
	}
	return s.Runtime.Snapshot().CapabilitySnapshot()
}
