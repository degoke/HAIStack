package auth

// ReadRequest asks whether a principal may read a FHIR resource.
type ReadRequest struct {
	Principal           Principal
	Tenant              TenantContext
	ResourceType        string
	ID                  string
	RequiredPermissions []string
}

// WriteRequest asks whether a principal may write a FHIR resource.
type WriteRequest struct {
	Principal           Principal
	Tenant              TenantContext
	Operation           string // create | update
	ResourceType        string
	ID                  string
	RequiredPermissions []string
}

// ViewRequest asks whether a principal may execute a view.
type ViewRequest struct {
	Principal           Principal
	Tenant              TenantContext
	ViewName            string
	Version             string
	ResourceType        string
	RequiredPermissions []string
	Parameters          map[string]any
}

// AIToolRequest asks whether a principal may execute an AI tool.
type AIToolRequest struct {
	Principal           Principal
	Tenant              TenantContext
	ToolName            string
	ResourceType        string
	ViewName            string
	RequiredPermissions []string
}

// DevicePushRequest asks whether a device may push sync events for a tenant.
type DevicePushRequest struct {
	DeviceID string
	TenantID string
}

// ModuleInstallRequest asks whether a principal may install a module.
type ModuleInstallRequest struct {
	Principal           Principal
	Tenant              TenantContext
	ModuleName          string
	ModuleVersion       string
	RequiredPermissions []string
}

// PatientScopeRequest asks whether a principal may access a patient within
// their tenant patient-scope stub.
type PatientScopeRequest struct {
	Principal Principal
	Tenant    TenantContext
	PatientID string
}

// evalInput is the internal evaluation context for the policy DSL.
type evalInput struct {
	Principal           Principal
	Tenant              TenantContext
	Permissions         []Permission
	Roles               []string
	Action              string
	ResourceType        string
	ResourceID          string
	ViewName            string
	ViewVersion         string
	ToolName            string
	ModuleName          string
	ModuleVersion       string
	Device              *DeviceIdentity
	RequiredPermissions []string
	PatientID           string
}
