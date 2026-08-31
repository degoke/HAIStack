package types

// ClientValidationOutcomeError marks errors that represent client-side validation
// failures with a structured OperationOutcome. Infrastructure and not-found errors
// should not implement this interface.
type ClientValidationOutcomeError interface {
	error
	OperationOutcome() OperationOutcome
}
