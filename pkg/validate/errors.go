package validate

import "errors"

// FailedError is returned by NewCoreValidator when one or more validation issues are found.
type FailedError struct {
	Issues []ValidationIssue
}

func (e FailedError) Error() string {
	return joinIssueDiagnostics(e.Issues)
}

// IssuesFromError returns structured validation issues when err is or wraps a FailedError.
func IssuesFromError(err error) ([]ValidationIssue, bool) {
	var failed FailedError
	if errors.As(err, &failed) {
		out := make([]ValidationIssue, len(failed.Issues))
		copy(out, failed.Issues)
		return out, true
	}
	return nil, false
}
