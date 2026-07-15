package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Error carries HTTP status, raw body, parsed OperationOutcome, and retry hints.
type Error struct {
	StatusCode int
	Body       []byte
	Outcome    *types.OperationOutcome
	Retryable  bool
	Message    string
}

func (e *Error) Error() string {
	if e == nil {
		return "client error"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Outcome != nil && len(e.Outcome.Issue) > 0 {
		return e.Outcome.Issue[0].Diagnostics
	}
	return fmt.Sprintf("request failed with status %d", e.StatusCode)
}

// IsRetryable reports whether the error is retryable.
func (e *Error) IsRetryable() bool {
	return e != nil && e.Retryable
}

// AsError unwraps or wraps an error as *Error.
func AsError(err error) (*Error, bool) {
	var ce *Error
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

func parseError(status int, body []byte, retryable bool) *Error {
	ce := &Error{
		StatusCode: status,
		Body:       append([]byte(nil), body...),
		Retryable:  retryable,
	}
	var outcome types.OperationOutcome
	if len(body) > 0 && json.Unmarshal(body, &outcome) == nil && outcome.ResourceType == "OperationOutcome" {
		ce.Outcome = &outcome
	}
	if ce.Outcome == nil {
		ce.Message = fmt.Sprintf("request failed with status %d", status)
	} else if len(ce.Outcome.Issue) > 0 && ce.Outcome.Issue[0].Diagnostics != "" {
		ce.Message = ce.Outcome.Issue[0].Diagnostics
	}
	return ce
}

func isSuccessStatus(code int) bool {
	return code >= 200 && code < 300
}

func defaultRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
