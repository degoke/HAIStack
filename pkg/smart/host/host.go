package host

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// Config configures SMART host routes mounted in front of a FHIR handler.
type Config struct {
	// FHIRBasePath is the SMART issuer and FHIR REST root (for example http://localhost:8765/fhir).
	FHIRBasePath string
	// OAuthBasePath is the prefix for stub OAuth endpoints (for example http://localhost:8765/oauth).
	OAuthBasePath string
	// Configuration overrides fields in the served SMART metadata document.
	Configuration smart.Configuration
}

// Handler serves SMART well-known metadata and optional stub OAuth endpoints in
// front of a FHIR http.Handler. This is a reference host for Inferno discovery
// tests — not a production authorization server.
type Handler struct {
	fhir          http.Handler
	fhirPath      string
	wellKnownPath string
	oauthAuthPath string
	oauthTokenPath string
	wellKnown     []byte
}

// NewHandler returns a handler that routes SMART/OAuth paths before delegating
// to fhir for all other requests.
func NewHandler(fhir http.Handler, cfg Config) (*Handler, error) {
	if fhir == nil {
		return nil, errors.New("smart/host: fhir handler is required")
	}
	fhirBase := strings.TrimRight(strings.TrimSpace(cfg.FHIRBasePath), "/")
	oauthBase := strings.TrimRight(strings.TrimSpace(cfg.OAuthBasePath), "/")
	if fhirBase == "" || oauthBase == "" {
		return nil, errors.New("smart/host: FHIRBasePath and OAuthBasePath are required")
	}
	fhirPath, err := urlPath(fhirBase)
	if err != nil {
		return nil, err
	}
	oauthPath, err := urlPath(oauthBase)
	if err != nil {
		return nil, err
	}

	conf := cfg.Configuration
	if conf.Issuer == "" {
		conf.Issuer = fhirBase
	}
	if conf.AuthorizationEndpoint == "" {
		conf.AuthorizationEndpoint = oauthBase + "/authorize"
	}
	if conf.TokenEndpoint == "" {
		conf.TokenEndpoint = oauthBase + "/token"
	}
	if len(conf.ScopesSupported) == 0 {
		conf.ScopesSupported = smart.DefaultConfiguration(fhirBase).ScopesSupported
	}
	if len(conf.ResponseTypesSupported) == 0 {
		conf.ResponseTypesSupported = []string{"code"}
	}
	if len(conf.GrantTypesSupported) == 0 {
		conf.GrantTypesSupported = []string{"authorization_code", "client_credentials"}
	}
	if len(conf.Capabilities) == 0 {
		conf.Capabilities = infernoDiscoveryCapabilities()
	}
	if len(conf.CodeChallengeMethodsSupported) == 0 {
		conf.CodeChallengeMethodsSupported = []string{"S256"}
	}

	raw, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	return &Handler{
		fhir:           fhir,
		fhirPath:       fhirPath,
		wellKnownPath:  fhirPath + "/.well-known/smart-configuration",
		oauthAuthPath:  oauthPath + "/authorize",
		oauthTokenPath: oauthPath + "/token",
		wellKnown:      raw,
	}, nil
}

func urlPath(base string) (string, error) {
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		// Strip scheme and host; keep path only.
		withoutScheme := strings.SplitN(base, "://", 2)[1]
		if idx := strings.Index(withoutScheme, "/"); idx >= 0 {
			return withoutScheme[idx:], nil
		}
		return "/", nil
	}
	if !strings.HasPrefix(base, "/") {
		return "/" + base, nil
	}
	return base, nil
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.fhir == nil {
		http.Error(w, "handler not configured", http.StatusInternalServerError)
		return
	}
	path := r.URL.Path
	switch path {
	case h.wellKnownPath:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(h.wellKnown)
		return
	case h.oauthAuthPath:
		h.handleAuthorize(w, r)
		return
	case h.oauthTokenPath:
		h.handleToken(w, r)
		return
	}
	h.fhir.ServeHTTP(w, r)
}

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	if redirectURI == "" {
		http.Error(w, "redirect_uri required", http.StatusBadRequest)
		return
	}
	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	location := redirectURI + sep + "code=inferno-demo-code"
	if state != "" {
		location += "&state=" + state
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "inferno-demo-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        "patient/*.read launch/patient",
		"patient":      "demo-patient",
	})
}

// infernoDiscoveryCapabilities returns capabilities required by the Inferno
// SMART discovery STU2 group (without sso-openid-connect so issuer may be omitted).
func infernoDiscoveryCapabilities() []string {
	return []string{
		"launch-ehr",
		"launch-standalone",
		"client-public",
		"client-confidential-symmetric",
		"client-confidential-asymmetric",
		"context-ehr-patient",
		"context-standalone-patient",
		"permission-patient",
		"permission-user",
		"permission-v1",
		"permission-v2",
	}
}

// WellKnownJSON returns the served smart-configuration document.
func (h *Handler) WellKnownJSON() []byte {
	if h == nil {
		return nil
	}
	return h.wellKnown
}
