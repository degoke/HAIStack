package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

type consentSession struct {
	Params    url.Values
	CSRF      string
	Subject   string
	ExpiresAt time.Time
}

type consentSessionStore struct {
	mu       sync.Mutex
	sessions map[string]consentSession
	nowFn    func() time.Time
}

func newConsentSessionStore(nowFn func() time.Time) *consentSessionStore {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &consentSessionStore{
		sessions: make(map[string]consentSession),
		nowFn:    nowFn,
	}
}

func (s *consentSessionStore) create(params url.Values, subject string) (sessionID, csrf string, err error) {
	sessionID, err = randomURLSafeToken(24)
	if err != nil {
		return "", "", err
	}
	csrf, err = randomURLSafeToken(32)
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.sessions[sessionID] = consentSession{
		Params:    cloneValues(params),
		CSRF:      csrf,
		Subject:   subject,
		ExpiresAt: s.nowFn().Add(defaultConsentSessionTTL),
	}
	return sessionID, csrf, nil
}

func (s *consentSessionStore) consume(sessionID, csrf string) (url.Values, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrInvalidRequest
	}
	delete(s.sessions, sessionID)
	if s.nowFn().After(session.ExpiresAt) {
		return nil, ErrInvalidRequest
	}
	if csrf == "" || csrf != session.CSRF {
		return nil, ErrInvalidRequest
	}
	return cloneValues(session.Params), nil
}

func (s *consentSessionStore) purgeLocked() {
	now := s.nowFn()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
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
	sessionID, csrf, err := s.consentSessions.create(params, subject)
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

func (s *Server) loadConsentSession(r *http.Request, csrf string) (url.Values, error) {
	cookie, err := r.Cookie(s.consentCookieName())
	if err != nil || cookie.Value == "" {
		return nil, ErrInvalidRequest
	}
	if s.consentSessions == nil {
		return nil, ErrInvalidConfig
	}
	return s.consentSessions.consume(cookie.Value, csrf)
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func randomURLSafeToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
