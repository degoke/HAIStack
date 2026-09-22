package validate

import "github.com/degoke/haistack/pkg/types"

// ToOperationOutcome maps a ValidationResult into a FHIR OperationOutcome.
func ToOperationOutcome(result *ValidationResult) *types.OperationOutcome {
	if result == nil {
		return nil
	}
	if len(result.Issues) == 0 {
		return &types.OperationOutcome{
			ResourceType: "OperationOutcome",
		}
	}

	issues := make([]types.OperationIssue, len(result.Issues))
	for i, iss := range result.Issues {
		code := iss.Code
		if code == "" {
			code = "invalid"
		}
		severity := iss.Severity
		if severity == "" {
			severity = "error"
		}
		issues[i] = types.OperationIssue{
			Severity:    severity,
			Code:        code,
			Diagnostics: iss.Diagnostics,
			Expression:  append([]string(nil), iss.Expression...),
		}
	}

	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue:        issues,
	}
}
