package oauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// handleAuthorize implements the OAuth 2.0 authorization endpoint with PKCE.
// Interactive consent is shown when AutoApprove is false (the default).
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	q := r.URL.Query()
	consentSubject := ""
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
			return
		}
		if csrf := strings.TrimSpace(r.Form.Get("csrf_token")); csrf != "" {
			sessionParams, subject, err := s.loadConsentSession(r, csrf)
			if err != nil {
				writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid or expired consent session")
				return
			}
			consentSubject = subject
			sessionParams.Set("approved", strings.TrimSpace(r.Form.Get("approved")))
			q = sessionParams
		} else {
			q = r.Form
		}
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

	client, err := s.clients.Lookup(s.issuer, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if !client.redirectAllowed(redirectURI) {
		s.redirectError(w, r, redirectURI, "invalid_request", "redirect_uri mismatch", state)
		return
	}
	if s.pkceRequired(client) {
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
	encounter := ""
	user := client.DefaultUser
	tenant := client.TenantHint
	launchSetUser := false
	if launch != "" {
		if s.launches == nil {
			s.redirectError(w, r, redirectURI, "invalid_request", "launch not supported", state)
			return
		}
		launchCtx, err := s.launches.Consume(launch)
		if err != nil {
			s.redirectError(w, r, redirectURI, "invalid_request", "invalid launch token", state)
			return
		}
		if launchCtx.Issuer != s.issuer {
			s.redirectError(w, r, redirectURI, "invalid_request", "invalid launch token", state)
			return
		}
		if launchCtx.PatientID != "" {
			patient = launchCtx.PatientID
		}
		if launchCtx.EncounterID != "" {
			encounter = launchCtx.EncounterID
		}
		if launchCtx.UserID != "" {
			user = launchCtx.UserID
			launchSetUser = true
		}
		if launchCtx.TenantHint != "" {
			tenant = launchCtx.TenantHint
		}
	}
	if !launchSetUser && consentSubject != "" {
		user = consentSubject
	}
	if aud != "" && aud != s.fhirBase {
		s.redirectError(w, r, redirectURI, "invalid_request", "aud must match FHIR base URL", state)
		return
	}

	if !s.autoApprove {
		approved := strings.TrimSpace(q.Get("approved"))
		if approved == "" {
			subject, loggedIn := s.ensureConsentLogin(w, r)
			if !loggedIn {
				return
			}
			csrf, err := s.beginConsentSession(w, r, q, subject)
			if err != nil {
				writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to begin consent session")
				return
			}
			s.renderConsent(w, r, q, csrf, client)
			return
		}
		if approved != "yes" {
			s.redirectError(w, r, redirectURI, "access_denied", "resource owner denied the request", state)
			return
		}
	}

	code, err := s.codes.Issue(AuthCode{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               grantedScope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		State:               state,
		Patient:             patient,
		Encounter:           encounter,
		User:                user,
		TenantHint:          tenant,
		Issuer:              s.issuer,
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
