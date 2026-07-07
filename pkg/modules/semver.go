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
	if s == "" {
		return version{}, fmt.Errorf("empty version")
	}
	// Strip build metadata; it must not affect precedence.
	if i := strings.Index(s, "+"); i >= 0 {
		s = s[:i]
	}
	pre := ""
	if i := strings.Index(s, "-"); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q does not match major.minor.patch", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return version{}, fmt.Errorf("version %q has invalid major: %w", s, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return version{}, fmt.Errorf("version %q has invalid minor: %w", s, err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return version{}, fmt.Errorf("version %q has invalid patch: %w", s, err)
	}
	if major < 0 || minor < 0 || patch < 0 {
		return version{}, fmt.Errorf("version %q has negative component", s)
	}
	return version{major: major, minor: minor, patch: patch, preRelease: pre}, nil
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
		ai, aErr := strconv.Atoi(ap[i])
		bi, bErr := strconv.Atoi(bp[i])
		if aErr == nil && bErr == nil {
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
			continue
		}
		// Identifiers consisting of only digits are compared numerically;
		// otherwise lexically. A numeric identifier is always lower precedence
		// than a non-numeric one.
		if aErr == nil && bErr != nil {
			return -1
		}
		if aErr != nil && bErr == nil {
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
