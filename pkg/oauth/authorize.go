package oauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// handleAuthorize implements the OAuth 2.0 authorization endpoint with PKCE.
// v1 uses auto-approval for registered clients (demo/edge deployments).
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
			return
		}
		q = r.Form
	}

	clientID := strings.TrimSpace(q.Get("client_id"))
	redirectURI := strings.TrimSpace(q.Get("redirect_uri"))
	responseType := strings.TrimSpace(q.Get("response_type"))
	scope := strings.TrimSpace(q.Get("scope"))
	state := q.Get("state")
	codeChallenge := strings.TrimSpace(q.Get("code_challenge"))
	codeChallengeMethod := strings.TrimSpace(q.Get("code_challenge_method"))
	launch := strings.TrimSpace(q.Get("launch"))
	aud := strings.TrimSpace(q.Get("aud"))

	if clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}
	if responseType != "code" {
		s.redirectError(w, r, redirectURI, "unsupported_response_type", "response_type must be code", state)
		return
	}

	client, err := s.clients.Lookup(clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if !client.redirectAllowed(redirectURI) {
		s.redirectError(w, r, redirectURI, "invalid_request", "redirect_uri mismatch", state)
		return
	}
	if client.requiresPKCE() {
		if codeChallenge == "" {
			s.redirectError(w, r, redirectURI, "invalid_request", "code_challenge required for public clients", state)
			return
		}
		if strings.EqualFold(codeChallengeMethod, "plain") {
			s.redirectError(w, r, redirectURI, "invalid_request", "plain code_challenge_method is not allowed", state)
			return
		}
		if method := strings.ToUpper(codeChallengeMethod); method != "" && method != "S256" {
			s.redirectError(w, r, redirectURI, "invalid_request", "only S256 code_challenge_method is supported", state)
			return
		}
		if codeChallengeMethod == "" {
			codeChallengeMethod = "S256"
		}
	}

	grantedScope, err := client.authorizeScopes(scope)
	if err != nil {
		s.redirectError(w, r, redirectURI, "invalid_scope", err.Error(), state)
		return
	}

	patient := client.DefaultPatient
	user := client.DefaultUser
	tenant := client.TenantHint
	if launch != "" {
		// v1: launch parameter accepted but not resolved to an EHR context.
		_ = launch
	}
	if aud != "" && aud != s.fhirBase {
		s.redirectError(w, r, redirectURI, "invalid_request", "aud must match FHIR base URL", state)
		return
	}

	if !s.autoApprove {
		writeOAuthError(w, http.StatusNotImplemented, "access_denied", "interactive consent is not implemented; enable AutoApprove")
		return
	}

	code, err := s.codes.Issue(AuthCode{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               grantedScope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		State:               state,
		Patient:             patient,
		User:                user,
		TenantHint:          tenant,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue authorization code")
		return
	}

	redirect, err := buildRedirect(redirectURI, code, state)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func buildRedirect(redirectURI, code, state string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid redirect_uri")
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, desc, state string) {
	if redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, errCode, desc)
		return
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, errCode, desc)
		return
	}
	q := u.Query()
	q.Set("error", errCode)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
