package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// IGValidatorConfig configures conformance example validation.
type IGValidatorConfig struct {
	// BaseBundleRoot is the HL7 R4 bundle root (contains structure-definitions/).
	BaseBundleRoot string
	// IGResourcesDir contains SUSHI-generated IG resources.
	IGResourcesDir string
	ValidDir       string
	InvalidDir     string
}

// ValidateIG validates conformance examples using the built-in Go validator.
func ValidateIG(ctx context.Context, cfg IGValidatorConfig) error {
	if err := requireDir(cfg.IGResourcesDir, "IG resources"); err != nil {
		return err
	}
	if err := requireDir(cfg.ValidDir, "valid examples"); err != nil {
		return err
	}
	if err := requireDir(cfg.InvalidDir, "invalid examples"); err != nil {
		return err
	}

	catalog, err := loadProfileCatalog(cfg)
	if err != nil {
		return err
	}
	term, err := loadTerminology(ctx, cfg.IGResourcesDir)
	if err != nil {
		return fmt.Errorf("load terminology: %w", err)
	}
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return fmt.Errorf("fhirpath engine: %w", err)
	}
	engine, err := validate.NewEngine(validate.Config{
		ProfileCatalog: catalog,
		FHIRPath:       fp,
	})
	if err != nil {
		return fmt.Errorf("validate engine: %w", err)
	}

	validOpts := fullValidationOptions(term)

	var failures []string

	validEntries, err := os.ReadDir(cfg.ValidDir)
	if err != nil {
		return err
	}
	for _, entry := range validEntries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(cfg.ValidDir, entry.Name())
		env, err := readEnvelope(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("valid example %s: %v", entry.Name(), err))
			continue
		}
		result, err := engine.Validate(ctx, env, validOpts)
		if err != nil {
			failures = append(failures, fmt.Sprintf("valid example %s: %v", entry.Name(), err))
			continue
		}
		if !result.Valid {
			failures = append(failures, fmt.Sprintf("valid example %s failed validation:\n%s", entry.Name(), formatIssues(result.Issues)))
			continue
		}
		fmt.Printf("PASS valid %s\n", entry.Name())
	}

	invalidEntries, err := os.ReadDir(cfg.InvalidDir)
	if err != nil {
		return err
	}
	for _, entry := range invalidEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".json") || strings.HasSuffix(name, ".expected.json") {
			continue
		}
		examplePath := filepath.Join(cfg.InvalidDir, name)
		expectedPath := filepath.Join(cfg.InvalidDir, strings.TrimSuffix(name, filepath.Ext(name))+".expected.json")
		expected, err := readExpected(expectedPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid example %s: %v", name, err))
			continue
		}
		if !expected.MustFail {
			failures = append(failures, fmt.Sprintf("invalid example %s: expected sidecar must set mustFail: true", name))
			continue
		}
		if expected.Profile == "" {
			failures = append(failures, fmt.Sprintf("invalid example %s: expected sidecar must set profile", name))
			continue
		}
		env, err := readEnvelope(examplePath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid example %s: %v", name, err))
			continue
		}
		opts := profileOnlyValidationOptions(term, expected.Profile)
		result, err := engine.Validate(ctx, env, opts)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid example %s: %v", name, err))
			continue
		}
		if result.Valid {
			failures = append(failures, fmt.Sprintf("invalid example %s unexpectedly passed", name))
			continue
		}
		if !matchesExpectedIssues(result.Issues, expected) {
			failures = append(failures, fmt.Sprintf("invalid example %s did not match expected sidecar:\n%s", name, formatIssues(result.Issues)))
			continue
		}
		fmt.Printf("PASS invalid %s (failed as expected)\n", name)
	}

	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n\n"))
	}
	fmt.Println("all IG examples matched expected validator outcomes")
	return nil
}

func fullValidationOptions(term terminology.Service) validate.ValidateOptions {
	return validate.ValidateOptions{
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		ProfileConstraints:      true,
		Mode:                    validate.ValidationModeFull,
		Terminology:             term,
	}
}

func profileOnlyValidationOptions(term terminology.Service, profileURL string) validate.ValidateOptions {
	return validate.ValidateOptions{
		EnforceBaseProfile:      false,
		EnforceDeclaredProfiles: false,
		Profiles:                []string{profileURL},
		ProfileConstraints:      true,
		Mode:                    validate.ValidationModeFull,
		Terminology:             term,
	}
}

type expectedExample struct {
	MustFail            bool     `json:"mustFail"`
	Profile             string   `json:"profile"`
	ExpectedSubstrings  []string `json:"expectedSubstrings"`
	ExpectedCodes       []string `json:"expectedCodes"`
	ExpectedExpressions []string `json:"expectedExpressions"`
}

func matchesExpectedIssues(issues []validate.ValidationIssue, expected *expectedExample) bool {
	if len(expected.ExpectedCodes) > 0 || len(expected.ExpectedExpressions) > 0 {
		for _, code := range expected.ExpectedCodes {
			if !issueHasCode(issues, code) {
				return false
			}
		}
		for _, expr := range expected.ExpectedExpressions {
			if !issueHasExpression(issues, expr) {
				return false
			}
		}
		return true
	}
	if len(expected.ExpectedSubstrings) == 0 {
		return true
	}
	blob := issueBlob(issues)
	for _, needle := range expected.ExpectedSubstrings {
		if !strings.Contains(blob, strings.ToLower(needle)) {
			return false
		}
	}
	return true
}

func issueHasCode(issues []validate.ValidationIssue, code string) bool {
	for _, iss := range issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}

func issueHasExpression(issues []validate.ValidationIssue, expr string) bool {
	for _, iss := range issues {
		for _, got := range iss.Expression {
			if got == expr || strings.HasSuffix(got, "."+expr) || strings.Contains(got, expr) {
				return true
			}
		}
		if strings.Contains(strings.ToLower(iss.Diagnostics), strings.ToLower(expr)) {
			return true
		}
	}
	return false
}

func readExpected(path string) (*expectedExample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing expected sidecar %s", filepath.Base(path))
		}
		return nil, err
	}
	var expected expectedExample
	if err := json.Unmarshal(data, &expected); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return &expected, nil
}

func readEnvelope(path string) (*types.ResourceEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	resourceType, err := types.GetResourceType(data)
	if err != nil {
		return nil, err
	}
	return &types.ResourceEnvelope{
		ResourceType: resourceType,
		JSON:         data,
	}, nil
}

func requireDir(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s missing at %q; run make ig first", label, path)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s path %q is not a directory", label, path)
	}
	return nil
}

func issueBlob(issues []validate.ValidationIssue) string {
	var parts []string
	for _, iss := range issues {
		parts = append(parts, iss.Code, iss.Diagnostics, iss.Severity)
		parts = append(parts, iss.Expression...)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func formatIssues(issues []validate.ValidationIssue) string {
	if len(issues) == 0 {
		return "(no issues)"
	}
	lines := make([]string, 0, len(issues))
	for _, iss := range issues {
		lines = append(lines, fmt.Sprintf("[%s] %s (%s)", iss.Severity, iss.Code, iss.Diagnostics))
	}
	return strings.Join(lines, "\n")
}
