package auth

// PrincipalKind classifies a principal identity.
type PrincipalKind string

const (
	// KindUser is a human operator or clinician.
	KindUser PrincipalKind = "user"
	// KindDevice is an edge or sync device.
	KindDevice PrincipalKind = "device"
	// KindService is a backend service account.
	KindService PrincipalKind = "service"
	// KindAIAgent is an AI agent or tool-calling identity.
	KindAIAgent PrincipalKind = "ai-agent"
)

// Permission is a string-based capability declaration (e.g. "read-appointment",
// "appointment.read"). v1 keeps permissions as free-form strings consistent with
// module and view permission declarations.
type Permission string

// Role groups permissions under a named role.
type Role struct {
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions,omitempty"`
}

// TenantBinding links a principal to roles within one tenant.
type TenantBinding struct {
	TenantID string   `json:"tenantId"`
	Roles    []string `json:"roles,omitempty"`
}

// Principal is the generic identity model for authorization decisions.
type Principal struct {
	ID             string            `json:"id"`
	Kind           PrincipalKind     `json:"kind"`
	DisplayName    string            `json:"displayName,omitempty"`
	TenantBindings []TenantBinding   `json:"tenantBindings,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// TenantContext carries the active tenant, role bindings, purpose-of-use, and
// optional patient scope for one authorization decision.
type TenantContext struct {
	TenantID     string   `json:"tenantId"`
	RoleBindings []string `json:"roleBindings,omitempty"`
	PurposeOfUse string   `json:"purposeOfUse,omitempty"`
	PatientScope string   `json:"patientScope,omitempty"`
}

// DeviceStatus values for DeviceIdentity.Status.
const (
	DeviceStatusActive  = "active"
	DeviceStatusPending = "pending"
	DeviceStatusRevoked = "revoked"
)

// DeviceIdentity is a first-class device record used for sync trust checks.
type DeviceIdentity struct {
	DeviceID          string            `json:"deviceId"`
	TenantID          string            `json:"tenantId"`
	Status            string            `json:"status"`
	Trusted           bool              `json:"trusted"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	LinkedPrincipalID string            `json:"linkedPrincipalId,omitempty"`
}

// Decision is the outcome of an authorization check.
type Decision struct {
	Allowed             bool              `json:"allowed"`
	Reason              string            `json:"reason,omitempty"`
	RequiredPermissions []string          `json:"requiredPermissions,omitempty"`
	Filters             map[string]string `json:"filters,omitempty"`
	Constraints         map[string]string `json:"constraints,omitempty"`
	RequiresApproval    bool              `json:"requiresApproval,omitempty"`
}

// Deny returns a deny Decision with the given reason.
func Deny(reason string) Decision {
	return Decision{Allowed: false, Reason: reason}
}

// Allow returns an allow Decision with the given reason.
func Allow(reason string) Decision {
	return Decision{Allowed: true, Reason: reason}
}

// Action names used by decision APIs and the policy DSL.
const (
	ActionRead          = "read"
	ActionWrite         = "write"
	ActionSearch        = "search"
	ActionExecuteView   = "execute-view"
	ActionExecuteAITool = "execute-ai-tool"
	ActionPushDevice    = "push-device-event"
	ActionInstallModule = "install-module"
	ActionPatientAccess = "patient-access"
)
