package infernotest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// WellKnownURL returns the Inferno SMART discovery URL for a FHIR base endpoint.
func WellKnownURL(fhirBase string) string {
	return strings.TrimRight(strings.TrimSpace(fhirBase), "/") + "/.well-known/smart-configuration"
}

// FetchWellKnownConfiguration performs the GET request used by Inferno's
// well_known_endpoint test.
func FetchWellKnownConfiguration(ctx context.Context, client *http.Client, fhirBase string) ([]byte, http.Header, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, WellKnownURL(fhirBase), nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, resp.StatusCode, err
	}
	return body, resp.Header, resp.StatusCode, nil
}

// ParseWellKnownConfiguration parses a SMART discovery JSON document.
func ParseWellKnownConfiguration(raw []byte) (map[string]any, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse well-known configuration: %w", err)
	}
	return config, nil
}

// AssertWellKnownEndpoint mirrors Inferno's well_known_endpoint test.
func AssertWellKnownEndpoint(t *testing.T, status int, headers http.Header, raw []byte) map[string]any {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("well-known status = %d, want 200; body=%s", status, raw)
	}
	if len(raw) == 0 {
		t.Fatal("well-known configuration body is empty")
	}
	config, err := ParseWellKnownConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		t.Fatal("no Content-Type header received")
	}
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	for _, key := range []string{"authorization_endpoint", "token_endpoint"} {
		value, ok := config[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("well-known configuration missing %q", key)
		}
	}
	return config
}

// AssertWellKnownCapabilitiesSTU2 mirrors Inferno's well_known_capabilities_stu2 test.
func AssertWellKnownCapabilitiesSTU2(t *testing.T, config map[string]any) {
	t.Helper()
	if config == nil {
		t.Fatal("well-known configuration is nil")
	}
	required := map[string]string{
		"authorization_endpoint":           "string",
		"token_endpoint":                   "string",
		"capabilities":                     "array",
		"grant_types_supported":            "array",
		"code_challenge_methods_supported": "array",
	}
	for key, kind := range required {
		value, ok := config[key]
		if !ok {
			t.Fatalf("well-known configuration does not include %q", key)
		}
		if value == nil {
			t.Fatalf("well-known configuration field %q is blank", key)
		}
		switch kind {
		case "string":
			if _, ok := value.(string); !ok || strings.TrimSpace(value.(string)) == "" {
				t.Fatalf("well-known %q must be a non-empty string", key)
			}
		case "array":
			items, ok := value.([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("well-known %q must be a non-empty array", key)
			}
		}
	}

	grants, _ := config["grant_types_supported"].([]any)
	if !containsString(grants, "authorization_code") {
		t.Fatalf("grant_types_supported = %#v, want authorization_code", grants)
	}
	methods, _ := config["code_challenge_methods_supported"].([]any)
	if !containsString(methods, "S256") {
		t.Fatalf("code_challenge_methods_supported = %#v, want S256", methods)
	}
	if containsString(methods, "plain") {
		t.Fatalf("code_challenge_methods_supported must not include plain: %#v", methods)
	}

	capabilities, _ := config["capabilities"].([]any)
	for _, capability := range capabilities {
		if _, ok := capability.(string); !ok {
			t.Fatalf("capabilities must be strings, found %#v", capability)
		}
	}
	if containsString(capabilities, "sso-openid-connect") {
		issuer, ok := config["issuer"].(string)
		if !ok || strings.TrimSpace(issuer) == "" {
			t.Fatal("issuer must be present when capabilities includes sso-openid-connect")
		}
		jwksURI, ok := config["jwks_uri"].(string)
		if !ok || strings.TrimSpace(jwksURI) == "" {
			t.Fatal("jwks_uri must be present when capabilities includes sso-openid-connect")
		}
	}
}

func containsString(items []any, target string) bool {
	for _, item := range items {
		if s, ok := item.(string); ok && s == target {
			return true
		}
	}
	return false
}
