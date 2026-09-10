package golden

import "github.com/degoke/health-ai-stack/pkg/types"

// AuthOutcomeCatalog documents stable OperationOutcome shapes for authorization failures.
var AuthOutcomeCatalog = map[string]types.OperationOutcome{
	"forbidden_policy":             ForbiddenOutcome("policy denied"),
	"forbidden_patient_scope":      ForbiddenOutcome("principal scoped to patient \"pat-1\" cannot access \"pat-2\""),
	"forbidden_scope_filter":       ForbiddenOutcome("resource outside granted scope filters"),
	"security_unauthenticated":     SecurityOutcome("http: unauthenticated"),
	"security_token_expired":       SecurityOutcome("smart: token expired"),
	"security_token_not_yet_valid": SecurityOutcome("smart: token not yet valid"),
	"security_replay":              SecurityOutcome("smart: replayed assertion"),
}

// ForbiddenOutcome returns a 403-style OperationOutcome with the given diagnostics.
func ForbiddenOutcome(diagnostics string) types.OperationOutcome {
	return types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "forbidden",
			Diagnostics: diagnostics,
		}},
	}
}

// SecurityOutcome returns a 401-style OperationOutcome with the given diagnostics.
func SecurityOutcome(diagnostics string) types.OperationOutcome {
	return types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "security",
			Diagnostics: diagnostics,
		}},
	}
}
