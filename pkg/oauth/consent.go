package oauth

import (
	"context"
	"net/http"
)

// AuthorizationRequest captures parameters from the authorize endpoint.
type AuthorizationRequest struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	Patient             string
	Encounter           string
	Subject             string
	FHIRUser            string
	Launch              string
	CodeChallenge       string
	CodeChallengeMethod string
}

// ConsentHandler approves or denies authorization requests before codes are issued.
// When nil, the server auto-approves (development mode only).
type ConsentHandler interface {
	Approve(ctx context.Context, req AuthorizationRequest) (bool, error)
}

// ConsentHandlerFunc adapts a function to ConsentHandler.
type ConsentHandlerFunc func(ctx context.Context, req AuthorizationRequest) (bool, error)

// Approve implements ConsentHandler.
func (f ConsentHandlerFunc) Approve(ctx context.Context, req AuthorizationRequest) (bool, error) {
	if f == nil {
		return true, nil
	}
	return f(ctx, req)
}

// AutoApproveConsentHandler always approves authorization requests.
func AutoApproveConsentHandler() ConsentHandler {
	return ConsentHandlerFunc(func(context.Context, AuthorizationRequest) (bool, error) {
		return true, nil
	})
}

// ServeConsentPage writes a minimal HTML consent form for pending authorization requests.
func ServeConsentPage(w http.ResponseWriter, req AuthorizationRequest, csrfToken string) {
	contextLines := ""
	if req.Patient != "" {
		contextLines += `<p>Patient context: <strong>` + htmlEscape(req.Patient) + `</strong></p>`
	}
	if req.Encounter != "" {
		contextLines += `<p>Encounter context: <strong>` + htmlEscape(req.Encounter) + `</strong></p>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Authorize application</title></head>
<body>
<h1>Authorize access</h1>
<p>Client <strong>` + htmlEscape(req.ClientID) + `</strong> requests access with scope:</p>
<pre>` + htmlEscape(req.Scope) + `</pre>
` + contextLines + `
<form method="POST">
<input type="hidden" name="csrf_token" value="` + htmlEscape(csrfToken) + `">
<button name="approve" value="yes">Approve</button>
<button name="approve" value="no">Deny</button>
</form>
</body></html>`))
}
