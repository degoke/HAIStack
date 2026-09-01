package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// IGValidatorConfig configures conformance example validation.
type IGValidatorConfig struct {
	// BaseSDDir contains bundled HL7 R4 StructureDefinitions.
	BaseSDDir string
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

	catalog, err := validate.LoadProfileCatalogFromDirs(cfg.BaseSDDir, cfg.IGResourcesDir)
	if err != nil {
		return fmt.Errorf("load profile catalog: %w", err)
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

	valOpts := validate.ValidateOptions{
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		ProfileConstraints:      true,
		Mode:                    validate.ValidationModeFull,
	}

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
		result, err := engine.Validate(ctx, env, valOpts)
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
		env, err := readEnvelope(examplePath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid example %s: %v", name, err))
			continue
		}
		opts := valOpts
		if expected.Profile != "" {
			opts.Profiles = []string{expected.Profile}
			opts.EnforceDeclaredProfiles = false
		}
		result, err := engine.Validate(ctx, env, opts)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid example %s: %v", name, err))
			continue
		}
		if result.Valid {
			failures = append(failures, fmt.Sprintf("invalid example %s unexpectedly passed", name))
			continue
		}
		blob := issueBlob(result.Issues)
		matched := true
		for _, needle := range expected.ExpectedSubstrings {
			if !strings.Contains(blob, strings.ToLower(needle)) {
				failures = append(failures, fmt.Sprintf("invalid example %s did not mention %q:\n%s", name, needle, formatIssues(result.Issues)))
				matched = false
				break
			}
		}
		if matched {
			fmt.Printf("PASS invalid %s (failed as expected)\n", name)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n\n"))
	}
	fmt.Println("all IG examples matched expected validator outcomes")
	return nil
}

type expectedExample struct {
	MustFail            bool     `json:"mustFail"`
	Profile             string   `json:"profile"`
	ExpectedSubstrings  []string `json:"expectedSubstrings"`
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
