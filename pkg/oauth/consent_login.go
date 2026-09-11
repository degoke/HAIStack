package oauth

import "net/http"

// ConsentLoginHandler authenticates the resource owner before interactive consent.
// When nil, consent is shown without a separate login step (edge/demo deployments).
type ConsentLoginHandler interface {
	// EnsureLoggedIn writes a login response and returns ok=false when the user is not
	// authenticated. returnURL is the authorize URL to resume after login succeeds.
	EnsureLoggedIn(w http.ResponseWriter, r *http.Request, returnURL string) (subject string, ok bool)
}
