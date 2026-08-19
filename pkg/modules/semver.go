package modules

import (
	"fmt"
	"strconv"
	"strings"
)

// version is a minimal semver representation used for dependency checking.
// Build metadata is ignored during comparison.
type version struct {
	major      int
	minor      int
	patch      int
	preRelease string
}

// parseVersion parses a minimal semver string: major.minor.patch[-prerelease][+build].
// It rejects empty or obviously malformed versions so that bad manifests fail
// early.
func parseVersion(s string) (version, error) {
	original := s
	if s == "" {
		return version{}, fmt.Errorf("empty version")
	}
	if s != strings.TrimSpace(s) {
		return version{}, fmt.Errorf("version %q contains surrounding whitespace", original)
	}
	build := ""
	if parts := strings.Split(s, "+"); len(parts) > 1 {
		if len(parts) != 2 || parts[1] == "" || !validIdentifiers(parts[1], false) {
			return version{}, fmt.Errorf("version %q has invalid build metadata", original)
		}
		build = parts[1]
		s = parts[0]
	}
	pre := ""
	if parts := strings.SplitN(s, "-", 2); len(parts) == 2 {
		if parts[1] == "" || !validIdentifiers(parts[1], true) {
			return version{}, fmt.Errorf("version %q has invalid prerelease metadata", original)
		}
		pre = parts[1]
		s = parts[0]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q does not match major.minor.patch", original)
	}
	major, err := parseNumericCore(parts[0], original, "major")
	if err != nil {
		return version{}, err
	}
	minor, err := parseNumericCore(parts[1], original, "minor")
	if err != nil {
		return version{}, err
	}
	patch, err := parseNumericCore(parts[2], original, "patch")
	if err != nil {
		return version{}, err
	}
	if major < 0 || minor < 0 || patch < 0 {
		return version{}, fmt.Errorf("version %q has negative component", original)
	}
	_ = build // validated but deliberately ignored for precedence.
	return version{major: major, minor: minor, patch: patch, preRelease: pre}, nil
}

func parseNumericCore(value, original, component string) (int, error) {
	if value == "" || !allASCIIDigits(value) || (len(value) > 1 && value[0] == '0') {
		return 0, fmt.Errorf("version %q has invalid %s", original, component)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("version %q has invalid %s: %w", original, component, err)
	}
	return parsed, nil
}

func validIdentifiers(value string, rejectLeadingZeroes bool) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, r := range identifier {
			if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '-' {
				return false
			}
		}
		if rejectLeadingZeroes && allASCIIDigits(identifier) && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// compare returns -1, 0, or 1 using semver precedence rules for the numeric
// components and prerelease tags. Build metadata is ignored.
func (v version) compare(other version) int {
	if v.major != other.major {
		if v.major < other.major {
			return -1
		}
		return 1
	}
	if v.minor != other.minor {
		if v.minor < other.minor {
			return -1
		}
		return 1
	}
	if v.patch != other.patch {
		if v.patch < other.patch {
			return -1
		}
		return 1
	}
	// A version without a prerelease has higher precedence than one with a
	// prerelease if all other components are equal.
	if v.preRelease == "" && other.preRelease != "" {
		return 1
	}
	if v.preRelease != "" && other.preRelease == "" {
		return -1
	}
	if v.preRelease == "" && other.preRelease == "" {
		return 0
	}
	return comparePreRelease(v.preRelease, other.preRelease)
}

func comparePreRelease(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		aNumeric := allASCIIDigits(ap[i])
		bNumeric := allASCIIDigits(bp[i])
		if aNumeric && bNumeric {
			if len(ap[i]) != len(bp[i]) {
				if len(ap[i]) < len(bp[i]) {
					return -1
				}
				return 1
			}
			if ap[i] != bp[i] {
				if ap[i] < bp[i] {
					return -1
				}
				return 1
			}
			continue
		}
		// Identifiers consisting of only digits are compared numerically;
		// otherwise lexically. A numeric identifier is always lower precedence
		// than a non-numeric one.
		if aNumeric && !bNumeric {
			return -1
		}
		if !aNumeric && bNumeric {
			return 1
		}
		if ap[i] != bp[i] {
			if ap[i] < bp[i] {
				return -1
			}
			return 1
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return 0
}

// isCompatibleMinimum returns true if installed >= required.
func isCompatibleMinimum(installed, required string) (bool, error) {
	iv, err := parseVersion(installed)
	if err != nil {
		return false, fmt.Errorf("installed version %q: %w", installed, err)
	}
	rv, err := parseVersion(required)
	if err != nil {
		return false, fmt.Errorf("required version %q: %w", required, err)
	}
	return iv.compare(rv) >= 0, nil
}

// isGreaterVersion returns true if a > b.
func isGreaterVersion(a, b string) (bool, error) {
	av, err := parseVersion(a)
	if err != nil {
		return false, fmt.Errorf("version %q: %w", a, err)
	}
	bv, err := parseVersion(b)
	if err != nil {
		return false, fmt.Errorf("version %q: %w", b, err)
	}
	return av.compare(bv) > 0, nil
}
