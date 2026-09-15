package smart

import "errors"

// StableAuthDiagnostics returns catalog-stable diagnostics for SMART authentication errors.
// Dynamic suffixes such as exp/nbf timestamps are stripped so HTTP responses and golden
// tests can compare against AuthOutcomeCatalog entries.
func StableAuthDiagnostics(err error) string {
	switch {
	case errors.Is(err, ErrTokenExpired):
		return ErrTokenExpired.Error()
	case errors.Is(err, ErrTokenNotYetValid):
		return ErrTokenNotYetValid.Error()
	case errors.Is(err, ErrReplay):
		return ErrReplay.Error()
	case errors.Is(err, ErrUnauthorized):
		return ErrUnauthorized.Error()
	case errors.Is(err, ErrInvalidToken):
		return ErrInvalidToken.Error()
	case errors.Is(err, ErrIssuerMismatch):
		return ErrIssuerMismatch.Error()
	case errors.Is(err, ErrAudienceMismatch):
		return ErrAudienceMismatch.Error()
	case errors.Is(err, ErrMissingScopes):
		return ErrMissingScopes.Error()
	default:
		return err.Error()
	}
}
