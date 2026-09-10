package oauth

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultConsentSessionTTL = 5 * time.Minute
	defaultConsentCookieName = "haistack_oauth_consent"
	defaultConsentCookiePath = "/oauth/authorize"
)

const (
	defaultConsentPrimaryColor   = "#2563eb"
	defaultConsentPageBackground = "#f8fafc"
	defaultConsentCardBackground = "#ffffff"
	defaultConsentTextColor      = "#0f172a"
)

// ConsentUIConfig customizes the interactive authorization consent page.
type ConsentUIConfig struct {
	// Title is shown as the page heading. Defaults to "Authorize application".
	Title string
	// LogoURL optionally renders a branding image above the heading.
	LogoURL string
	// PrimaryColor sets accent color for buttons and links (CSS hex).
	PrimaryColor string
	// PageBackground sets the page background color (CSS hex).
	PageBackground string
	// CardBackground sets the consent card background color (CSS hex).
	CardBackground string
	// TextColor sets body text color (CSS hex).
	TextColor string
	// FooterText optionally renders muted text below the consent form.
	FooterText string
	// SessionCookieName overrides the consent session cookie name.
	SessionCookieName string
}

func (s *Server) consentUI() ConsentUIConfig {
	return s.cfg.ConsentUI
}

func (s *Server) consentCookieName() string {
	if name := strings.TrimSpace(s.consentUI().SessionCookieName); name != "" {
		return name
	}
	return defaultConsentCookieName
}

func (s *Server) consentCookiePath() string {
	u, err := url.Parse(s.issuer)
	if err != nil || u.Path == "" || u.Path == "/" {
		return defaultConsentCookiePath
	}
	return strings.TrimSuffix(u.Path, "/") + "/oauth/authorize"
}

func (s *Server) beginConsentSession(w http.ResponseWriter, r *http.Request, params url.Values, subject string) (string, error) {
	if s.consentSessions == nil {
		return "", ErrInvalidConfig
	}
	sessionID, csrf, err := s.consentSessions.Create(s.issuer, params, subject)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.consentCookieName(),
		Value:    sessionID,
		Path:     s.consentCookiePath(),
		MaxAge:   int(defaultConsentSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	return csrf, nil
}

func (s *Server) loadConsentSession(r *http.Request, csrf string) (url.Values, string, error) {
	cookie, err := r.Cookie(s.consentCookieName())
	if err != nil || cookie.Value == "" {
		return nil, "", ErrInvalidRequest
	}
	if s.consentSessions == nil {
		return nil, "", ErrInvalidConfig
	}
	return s.consentSessions.Consume(s.issuer, cookie.Value, csrf)
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
