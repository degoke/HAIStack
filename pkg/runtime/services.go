package runtime

import (
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// ServiceContainer exposes the wired service graph for embedding and testing.
type ServiceContainer struct {
	RegistryManager    *registry.Manager
	RegistrySnapshot   *registry.Snapshot
	ConformanceRuntime *ConformanceRuntime
	ModuleManager      *modules.Manager
	ResourceService    *core.ResourceService
	SearchService      *search.Service
	SyncEngine         *hasync.Engine
	FHIRPathEngine     fhirpath.Engine
	TerminologyService terminology.Service

	// TenantDB is set for Postgres modes.
	TenantDB *postgres.TenantDB

	// External adapters are populated in cloud mode when configured.
	BlobStore      BlobStoreAdapter
	ExternalSearch ExternalSearchAdapter
	Warehouse      WarehouseAdapter
}
