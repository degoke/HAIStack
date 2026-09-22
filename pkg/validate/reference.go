package validate

import (
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/types"
)

func validateReferenceSyntax(ref types.Reference) *ValidationIssue {
	raw := strings.TrimSpace(ref.Raw)
	if raw == "" {
		return &ValidationIssue{
			Severity:    "error",
			Code:        "invalid-reference",
			Diagnostics: "reference must not be empty",
			Expression:  []string{"Resource.reference"},
		}
	}

	if isAbsoluteOrSpecialReference(raw) {
		return nil
	}

	if ref.ResourceType != "" && ref.ID != "" {
		return nil
	}

	if strings.Contains(raw, "/") {
		return &ValidationIssue{
			Severity:    "error",
			Code:        "invalid-reference",
			Diagnostics: fmt.Sprintf("reference %q is not a valid typed relative, URL, URN, or fragment reference", raw),
			Expression:  []string{"Resource.reference"},
		}
	}

	return nil
}

func isAbsoluteOrSpecialReference(raw string) bool {
	return strings.HasPrefix(raw, "http://") ||
		strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "urn:") ||
		strings.HasPrefix(raw, "#")
}
