package core

import (
	"testing"
)

func TestTerminologyScopeForRoutesCodeSystemToGlobal(t *testing.T) {
	svc := &ResourceService{
		terminologyScope:       "tenant-a",
		globalTerminologyScope: "__global__",
	}
	if scope := svc.terminologyScopeFor("CodeSystem"); scope != "__global__" {
		t.Fatalf("scope=%q want global", scope)
	}
	if scope := svc.terminologyScopeFor("ValueSet"); scope != "tenant-a" {
		t.Fatalf("scope=%q want tenant-a", scope)
	}
}
