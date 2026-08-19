package modules

import "testing"

func TestParseVersionRejectsNonSemverForms(t *testing.T) {
	for _, input := range []string{
		"01.0.0",
		"1.0.0-",
		"1.0.0-alpha..1",
		"1.0.0+",
		"1.0.0+build+extra",
		"1.0.0-01",
	} {
		if _, err := parseVersion(input); err == nil {
			t.Errorf("parseVersion(%q) succeeded; want error", input)
		}
	}
}

func TestParseVersionAcceptsValidPrereleaseAndBuildMetadata(t *testing.T) {
	for _, input := range []string{
		"1.2.3",
		"1.2.3-alpha.1",
		"1.2.3-alpha-1+build.007",
	} {
		if _, err := parseVersion(input); err != nil {
			t.Errorf("parseVersion(%q) = %v, want success", input, err)
		}
	}
}
