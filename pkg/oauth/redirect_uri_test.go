package oauth

import "testing"

func TestValidateRedirectURI(t *testing.T) {
	if err := validateRedirectURI("https://app.example/callback"); err != nil {
		t.Fatalf("https redirect: %v", err)
	}
	if err := validateRedirectURI("http://127.0.0.1/callback"); err != nil {
		t.Fatalf("loopback http redirect: %v", err)
	}
	if err := validateRedirectURI("http://app.example/callback"); err == nil {
		t.Fatal("expected non-loopback http redirect to fail")
	}
	if err := validateRedirectURI("custom://app/callback"); err == nil {
		t.Fatal("expected custom scheme redirect to fail")
	}
}

func TestApplyProductionDefaults(t *testing.T) {
	cfg := Config{Issuer: "http://example.test"}
	if err := ApplyProductionDefaults(&cfg); err == nil {
		t.Fatal("expected production defaults to require https issuer")
	}
	cfg = Config{
		Issuer:                  "https://example.test",
		RegistrationAccessToken: "registration-token",
	}
	if err := ApplyProductionDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	autoApprove := true
	cfg.AutoApprove = &autoApprove
	if err := ApplyProductionDefaults(&cfg); err == nil {
		t.Fatal("expected AutoApprove true to fail production defaults")
	}
	cfg.AutoApprove = boolPtr(false)
	if err := ApplyProductionDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error with AutoApprove false: %v", err)
	}
	disabled := false
	cfg.DynamicClientRegistration = &disabled
	cfg.RegistrationAccessToken = ""
	if err := ApplyProductionDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error when DCR disabled: %v", err)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
