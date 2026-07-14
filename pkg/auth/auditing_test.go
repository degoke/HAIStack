package auth_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/auth"
)

func TestAuditingEngineEmitsDecisions(t *testing.T) {
	eng, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"appointment.read"},
		}},
		Principals: []auth.Principal{{
			ID:   "user-1",
			Kind: auth.KindUser,
			TenantBindings: []auth.TenantBinding{{
				TenantID: "tenant-a",
				Roles:    []string{"clinician"},
			}},
		}},
		PolicyBytes: []byte(`{
			"version": "1",
			"rules": [{
				"name": "appointment-read",
				"effect": "allow",
				"match": {
					"actions": ["read"],
					"resourceTypes": ["Appointment"],
					"anyPermissions": ["appointment.read"]
				}
			}]
		}`),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	mem := audit.NewMemoryStore()
	wrapper := &auth.AuditingEngine{
		Inner: eng,
		Audit: &audit.StoreAdapter{Store: mem},
	}

	principal, err := eng.Catalog().GetPrincipal("user-1")
	if err != nil {
		t.Fatalf("GetPrincipal: %v", err)
	}
	d, err := wrapper.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    principal,
		Tenant:       auth.TenantContext{TenantID: "tenant-a"},
		ResourceType: "Appointment",
		ID:           "appt-1",
	})
	if err != nil {
		t.Fatalf("CanReadResource: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %#v", d)
	}
	recs := mem.Records()
	if len(recs) != 1 || recs[0].Action != audit.ActionAuthAllow {
		t.Fatalf("audit = %#v", recs)
	}

	d, err = wrapper.CanReadResource(context.Background(), auth.ReadRequest{
		Principal:    principal,
		Tenant:       auth.TenantContext{TenantID: "tenant-a"},
		ResourceType: "Patient",
		ID:           "pat-1",
	})
	if err != nil {
		t.Fatalf("CanReadResource deny path: %v", err)
	}
	if d.Allowed {
		t.Fatal("expected deny")
	}
	recs = mem.Records()
	if len(recs) != 2 || recs[1].Action != audit.ActionAuthDeny {
		t.Fatalf("audit after deny = %#v", recs)
	}
}
