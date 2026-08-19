package validate

import (
	"errors"
	"testing"
)

func TestFailedErrorIssuesFromError(t *testing.T) {
	err := FailedError{Issues: []ValidationIssue{{
		Code:        "invalid-id",
		Diagnostics: "bad id",
		Expression:  []string{"Resource.id"},
	}}}

	issues, ok := IssuesFromError(err)
	if !ok || len(issues) != 1 || issues[0].Code != "invalid-id" {
		t.Fatalf("IssuesFromError direct = %+v, ok=%v", issues, ok)
	}

	wrapped := errors.Join(errors.New("outer"), err)
	issues, ok = IssuesFromError(wrapped)
	if !ok || len(issues) != 1 {
		t.Fatalf("IssuesFromError wrapped = %+v, ok=%v", issues, ok)
	}
}

func TestStructuralDiagnostics(t *testing.T) {
	got := structuralDiagnostics(errors.New(`error at "Patient.id": invalid Id format`))
	want := "Patient.id: invalid Id format"
	if got != want {
		t.Fatalf("structuralDiagnostics = %q, want %q", got, want)
	}
}
