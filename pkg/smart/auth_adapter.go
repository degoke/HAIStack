package smart

import (
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
)

// AuthBundle is the SMART-derived input ready for haistack-auth decision APIs.
type AuthBundle struct {
	Principal   auth.Principal
	Tenant      auth.TenantContext
	Permissions []auth.Permission
	Scopes      ScopeSet
	Launch      LaunchContext
	Claims      TokenClaims
	Client      *BackendClient
}

// AuthAdapterConfig configures SMART → auth translation.
type AuthAdapterConfig struct {
	// DefaultTenantID is used when launch/claims do not supply a tenant hint.
	DefaultTenantID string
	// DefaultUserRoles are applied to user principals when RoleBindings are empty.
	DefaultUserRoles []string
	// DefaultServiceRoles are applied to service principals when RoleBindings are empty.
	DefaultServiceRoles []string
	// PermissionMapper overrides default scope→permission derivation.
	PermissionMapper func(ScopeSet) []auth.Permission
}

// AuthAdapter translates SMART scopes, launch context, and token claims into
// pkg/auth principals, tenant context, permissions, and request builders.
// Authorization decisions remain in pkg/auth; this adapter only interprets SMART.
type AuthAdapter struct {
	cfg AuthAdapterConfig
}

// NewAuthAdapter returns an AuthAdapter.
func NewAuthAdapter(cfg AuthAdapterConfig) *AuthAdapter {
	return &AuthAdapter{cfg: cfg}
}

// ToAuthRequests builds an AuthBundle from token claims and launch context.
func (a *AuthAdapter) ToAuthRequests(claims TokenClaims, launch LaunchContext) (AuthBundle, error) {
	if a == nil {
		return AuthBundle{}, fmt.Errorf("%w: auth adapter is nil", ErrInvalidConfig)
	}
	scopes := claims.Scopes
	if scopes.Empty() && claims.Scope != "" {
		parsed, err := ParseScopes(claims.Scope)
		if err != nil {
			return AuthBundle{}, err
		}
		scopes = parsed
	}
	claims.Scopes = scopes
	launch = mergeLaunch(launch, claims)
	kind := principalKindFromScopes(scopes)
	id := principalID(claims, launch, kind)
	if id == "" {
		return AuthBundle{}, fmt.Errorf("%w: principal id required (sub, client_id, or launch user)", ErrInvalidConfig)
	}

	tenantID := firstNonEmpty(launch.TenantHint, claims.TenantHint, a.cfg.DefaultTenantID)
	if tenantID == "" {
		return AuthBundle{}, fmt.Errorf("%w: tenant id required (tenant hint or DefaultTenantID)", ErrInvalidConfig)
	}

	roles := a.cfg.DefaultUserRoles
	if kind == auth.KindService {
		roles = a.cfg.DefaultServiceRoles
	}

	principal := auth.Principal{
		ID:   id,
		Kind: kind,
		Attributes: map[string]string{
			"smart.scope": scopes.SpaceSeparated(),
		},
	}
	if claims.FHIRUser != "" {
		principal.Attributes["smart.fhirUser"] = claims.FHIRUser
	}
	if kind == auth.KindService {
		principal.DisplayName = claims.ClientID
	}
	if tenantID != "" && len(roles) > 0 {
		principal.TenantBindings = []auth.TenantBinding{{
			TenantID: tenantID,
			Roles:    append([]string(nil), roles...),
		}}
	}

	tenant := auth.TenantContext{
		TenantID:     tenantID,
		RoleBindings: append([]string(nil), roles...),
		PatientScope: launch.PatientID,
	}

	perms := a.PermissionsFromScopes(scopes)
	return AuthBundle{
		Principal:   principal,
		Tenant:      tenant,
		Permissions: perms,
		Scopes:      scopes,
		Launch:      launch,
		Claims:      claims,
	}, nil
}

// FromBackendService maps a validated backend assertion to an AuthBundle with a
// service principal.
func (a *AuthAdapter) FromBackendService(claims TokenClaims, client BackendClient, launch LaunchContext) (AuthBundle, error) {
	if claims.ClientID == "" {
		claims.ClientID = client.ClientID
	}
	if claims.Subject == "" {
		claims.Subject = client.ClientID
	}
	if claims.TenantHint == "" {
		claims.TenantHint = client.TenantHint
	}
	// Ensure system scopes drive KindService even when scopes are empty but client is backend.
	if claims.Scopes.Empty() && len(client.AllowedScopes) > 0 {
		// Do not invent scopes; principal kind falls back via force below.
	}
	bundle, err := a.ToAuthRequests(claims, launch)
	if err != nil {
		return AuthBundle{}, err
	}
	bundle.Principal.Kind = auth.KindService
	if client.DisplayName != "" {
		bundle.Principal.DisplayName = client.DisplayName
	} else if bundle.Principal.DisplayName == "" {
		bundle.Principal.DisplayName = client.ClientID
	}
	if len(a.cfg.DefaultServiceRoles) > 0 {
		bundle.Tenant.RoleBindings = append([]string(nil), a.cfg.DefaultServiceRoles...)
		bundle.Principal.TenantBindings = []auth.TenantBinding{{
			TenantID: bundle.Tenant.TenantID,
			Roles:    append([]string(nil), a.cfg.DefaultServiceRoles...),
		}}
	}
	c := client
	bundle.Client = &c
	return bundle, nil
}

// PermissionsFromScopes derives auth.Permission values from SMART resource scopes.
// Default form is "{ResourceType}.{verb}" or "*.{verb}" for wildcards.
func (a *AuthAdapter) PermissionsFromScopes(scopes ScopeSet) []auth.Permission {
	if a != nil && a.cfg.PermissionMapper != nil {
		return a.cfg.PermissionMapper(scopes)
	}
	return DefaultPermissionsFromScopes(scopes)
}

// DefaultPermissionsFromScopes maps resource scopes to auth permissions.
func DefaultPermissionsFromScopes(scopes ScopeSet) []auth.Permission {
	seen := make(map[auth.Permission]struct{})
	var out []auth.Permission
	add := func(p auth.Permission) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, sc := range scopes.ResourceScopes() {
		res := sc.Resource
		switch sc.Verb {
		case VerbRead:
			add(auth.Permission(res + ".read"))
		case VerbWrite:
			add(auth.Permission(res + ".write"))
		case VerbAll:
			add(auth.Permission(res + ".read"))
			add(auth.Permission(res + ".write"))
		}
	}
	return out
}

// ToReadRequest builds an auth.ReadRequest using SMART-derived identity and
// permissions derived from scopes for the resource type.
func (a *AuthAdapter) ToReadRequest(bundle AuthBundle, resourceType, id string) auth.ReadRequest {
	return auth.ReadRequest{
		Principal:           bundle.Principal,
		Tenant:              bundle.Tenant,
		ResourceType:        resourceType,
		ID:                  id,
		RequiredPermissions: requiredFor(bundle.Permissions, resourceType, VerbRead),
	}
}

// ToWriteRequest builds an auth.WriteRequest.
func (a *AuthAdapter) ToWriteRequest(bundle AuthBundle, operation, resourceType, id string) auth.WriteRequest {
	return auth.WriteRequest{
		Principal:           bundle.Principal,
		Tenant:              bundle.Tenant,
		Operation:           operation,
		ResourceType:        resourceType,
		ID:                  id,
		RequiredPermissions: requiredFor(bundle.Permissions, resourceType, VerbWrite),
	}
}

// ToPatientScopeRequest builds an auth.PatientScopeRequest using launch patient id
// when patientID is empty.
func (a *AuthAdapter) ToPatientScopeRequest(bundle AuthBundle, patientID string) auth.PatientScopeRequest {
	if patientID == "" {
		patientID = bundle.Launch.PatientID
	}
	return auth.PatientScopeRequest{
		Principal: bundle.Principal,
		Tenant:    bundle.Tenant,
		PatientID: patientID,
	}
}

// ScopeImplies reports whether the bundle's scopes authorize a resource action
// for the principal's SMART actor class.
func (a *AuthAdapter) ScopeImplies(bundle AuthBundle, resourceType string, verb AccessVerb) bool {
	actor := actorForKind(bundle.Principal.Kind, bundle.Scopes)
	if actor == "" {
		// Try all actor classes present in the scope set.
		for _, sc := range bundle.Scopes.ResourceScopes() {
			if sc.Matches(resourceType, verb) {
				return true
			}
		}
		return false
	}
	return bundle.Scopes.Allows(actor, resourceType, verb)
}

func requiredFor(perms []auth.Permission, resourceType string, verb AccessVerb) []string {
	wantSpecific := resourceType + "." + string(verb)
	wantWild := "*." + string(verb)
	var out []string
	for _, p := range perms {
		s := string(p)
		if strings.EqualFold(s, wantSpecific) || s == wantWild {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{wantSpecific}
	}
	return out
}

func principalKindFromScopes(scopes ScopeSet) auth.PrincipalKind {
	hasSystem := len(scopes.ResourceScopes(ActorSystem)) > 0
	hasUser := len(scopes.ResourceScopes(ActorUser)) > 0
	hasPatient := len(scopes.ResourceScopes(ActorPatient)) > 0
	switch {
	case hasSystem && !hasUser && !hasPatient:
		return auth.KindService
	case hasUser || hasPatient:
		return auth.KindUser
	default:
		return auth.KindUser
	}
}

func actorForKind(kind auth.PrincipalKind, scopes ScopeSet) ActorClass {
	switch kind {
	case auth.KindService:
		return ActorSystem
	case auth.KindUser:
		if len(scopes.ResourceScopes(ActorUser)) > 0 {
			return ActorUser
		}
		if len(scopes.ResourceScopes(ActorPatient)) > 0 {
			return ActorPatient
		}
		return ActorUser
	default:
		return ""
	}
}

func principalID(claims TokenClaims, launch LaunchContext, kind auth.PrincipalKind) string {
	switch kind {
	case auth.KindService:
		return firstNonEmpty(claims.ClientID, claims.Subject, claims.Issuer)
	default:
		return firstNonEmpty(launch.UserID, claims.FHIRUser, claims.Subject, claims.ClientID)
	}
}

func mergeLaunch(launch LaunchContext, claims TokenClaims) LaunchContext {
	return BuildLaunchContext(LaunchContextInput{
		PatientID:   launch.PatientID,
		EncounterID: launch.EncounterID,
		UserID:      launch.UserID,
		TenantHint:  launch.TenantHint,
		Metadata:    launch.Metadata,
		Scopes:      claims.Scopes,
		Claims:      &claims,
	})
}
