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
	cfg := Config{}
	if err := ApplyProductionDefaults(&cfg); err == nil {
		t.Fatal("expected production defaults to require registration token")
	}
	cfg.RegistrationAccessToken = "registration-token"
	if err := ApplyProductionDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	disabled := false
	cfg.DynamicClientRegistration = &disabled
	if err := ApplyProductionDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error when DCR disabled: %v", err)
	}
}
