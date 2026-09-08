package infernotest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DiscoveryResult captures SMART discovery checks aligned with the Inferno
// smart_discovery_stu2 group's well-known endpoint and capabilities tests.
type DiscoveryResult struct {
	WellKnownURL string
	StatusCode   int
	ContentType  string
	Config       map[string]any
	Errors       []string
}

// AssertDiscoverySTU2 performs Inferno-aligned SMART discovery checks against
// the FHIR endpoint base URL (for example http://localhost:8765/fhir).
func AssertDiscoverySTU2(client *http.Client, fhirBaseURL string) DiscoveryResult {
	result := DiscoveryResult{WellKnownURL: strings.TrimRight(fhirBaseURL, "/") + "/.well-known/smart-configuration"}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(result.WellKnownURL)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	result.StatusCode = resp.StatusCode
	result.ContentType = resp.Header.Get("Content-Type")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	if resp.StatusCode != http.StatusOK {
		result.Errors = append(result.Errors, fmt.Sprintf("status %d", resp.StatusCode))
		return result
	}
	if !strings.HasPrefix(strings.ToLower(result.ContentType), "application/json") {
		result.Errors = append(result.Errors, fmt.Sprintf("content-type %q", result.ContentType))
	}
	var config map[string]any
	if err := json.Unmarshal(body, &config); err != nil {
		result.Errors = append(result.Errors, "invalid json: "+err.Error())
		return result
	}
	result.Config = config
	validateCapabilities(config, &result)
	return result
}

func validateCapabilities(config map[string]any, result *DiscoveryResult) {
	required := map[string]string{
		"authorization_endpoint":          "string",
		"token_endpoint":                  "string",
		"capabilities":                    "array",
		"grant_types_supported":           "array",
		"code_challenge_methods_supported": "array",
	}
	for key, kind := range required {
		val, ok := config[key]
		if !ok {
			result.Errors = append(result.Errors, "missing `"+key+"`")
			continue
		}
		if val == nil || val == "" {
			result.Errors = append(result.Errors, "`"+key+"` is blank")
			continue
		}
		switch kind {
		case "string":
			if _, ok := val.(string); !ok || strings.TrimSpace(val.(string)) == "" {
				result.Errors = append(result.Errors, "`"+key+"` must be a non-empty string")
			}
		case "array":
			arr, ok := val.([]any)
			if !ok || len(arr) == 0 {
				result.Errors = append(result.Errors, "`"+key+"` must be a non-empty array")
			}
		}
	}

	grants, _ := config["grant_types_supported"].([]any)
	hasAuthCode := false
	for _, g := range grants {
		if g == "authorization_code" {
			hasAuthCode = true
		}
	}
	if !hasAuthCode {
		result.Errors = append(result.Errors, "grant_types_supported must include authorization_code")
	}

	methods, _ := config["code_challenge_methods_supported"].([]any)
	hasS256, hasPlain := false, false
	for _, m := range methods {
		switch m {
		case "S256":
			hasS256 = true
		case "plain":
			hasPlain = true
		}
	}
	if !hasS256 {
		result.Errors = append(result.Errors, "code_challenge_methods_supported must include S256")
	}
	if hasPlain {
		result.Errors = append(result.Errors, "code_challenge_methods_supported must not include plain")
	}

	caps, _ := config["capabilities"].([]any)
	for _, c := range caps {
		if _, ok := c.(string); !ok {
			result.Errors = append(result.Errors, "capabilities must be an array of strings")
			break
		}
	}

	hasSSO := false
	for _, c := range caps {
		if c == "sso-openid-connect" {
			hasSSO = true
		}
	}
	if hasSSO {
		if iss, ok := config["issuer"].(string); !ok || iss == "" {
			result.Errors = append(result.Errors, "issuer required when sso-openid-connect capability is present")
		}
		if jwks, ok := config["jwks_uri"].(string); !ok || jwks == "" {
			result.Errors = append(result.Errors, "jwks_uri required when sso-openid-connect capability is present")
		}
	}
}
