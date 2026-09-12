package jobs

import "testing"

func TestStampOwnerRoundTrip(t *testing.T) {
	payload, err := StampOwner([]byte(`{"path":"/mods/core"}`), JobOwner{
		PrincipalID: "user-1",
		TenantID:    "tenant-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := OwnerFromPayload(payload)
	if !ok || owner.PrincipalID != "user-1" || owner.TenantID != "tenant-a" {
		t.Fatalf("owner=%+v ok=%v", owner, ok)
	}
}
