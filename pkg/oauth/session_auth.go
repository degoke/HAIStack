package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const sessionCookieName = "haistack_oauth_session"
const sessionSecretEnv = "OAUTH_SESSION_SECRET"

// SessionUserAuthenticator authenticates users from signed session cookies.
type SessionUserAuthenticator struct {
	secret []byte
	now    func() time.Time
	ttl    time.Duration
}

// SessionAuthConfig configures signed cookie sessions for production consent login.
type SessionAuthConfig struct {
	Secret string
	TTL    time.Duration
	Now    func() time.Time
}

// NewSessionUserAuthenticator constructs a cookie session authenticator.
func NewSessionUserAuthenticator(cfg SessionAuthConfig) (*SessionUserAuthenticator, error) {
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv(sessionSecretEnv))
	}
	if secret == "" {
		return nil, fmt.Errorf("%w: set %s for production user login", ErrInvalidConfig, sessionSecretEnv)
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &SessionUserAuthenticator{
		secret: []byte(secret),
		now:    now,
		ttl:    ttl,
	}, nil
}

func (a *SessionUserAuthenticator) AuthenticateUser(r *http.Request) (UserIdentity, bool) {
	if a == nil {
		return UserIdentity{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return UserIdentity{}, false
	}
	subject, ok := a.verifySession(cookie.Value)
	if !ok {
		return UserIdentity{}, false
	}
	return UserIdentity{Subject: subject}, true
}

func (a *SessionUserAuthenticator) SetSession(w http.ResponseWriter, subject string) error {
	if a == nil || strings.TrimSpace(subject) == "" {
		return fmt.Errorf("oauth: session subject required")
	}
	value, err := a.signSession(subject)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		Expires:  a.now().Add(a.ttl),
	})
	return nil
}

type sessionPayload struct {
	Subject string `json:"sub"`
	Exp     int64  `json:"exp"`
}

func (a *SessionUserAuthenticator) signSession(subject string) (string, error) {
	payload := sessionPayload{
		Subject: strings.TrimSpace(subject),
		Exp:     a.now().Add(a.ttl).Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func (a *SessionUserAuthenticator) verifySession(value string) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	if payload.Exp > 0 && a.now().Unix() > payload.Exp {
		return "", false
	}
	subject := strings.TrimSpace(payload.Subject)
	if subject == "" {
		return "", false
	}
	return subject, true
}

func (s *Server) handleSessionLogin(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.cfg.UserAuthenticator.(*SessionUserAuthenticator)
	if !ok || auth == nil {
		http.NotFound(w, r)
		return
	}
	returnURL := strings.TrimSpace(r.URL.Query().Get("return"))
	if r.Method == http.MethodGet {
		if _, loggedIn := s.authenticatedUser(r); loggedIn && returnURL != "" {
			http.Redirect(w, r, returnURL, http.StatusFound)
			return
		}
		serveLoginPage(w, returnURL)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	subject := strings.TrimSpace(r.FormValue("username"))
	if subject == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}
	if err := auth.SetSession(w, subject); err != nil {
		http.Error(w, "session unavailable", http.StatusInternalServerError)
		return
	}
	if returnURL == "" {
		returnURL = s.cfg.Issuer + "/oauth/authorize"
	}
	http.Redirect(w, r, returnURL, http.StatusFound)
}

func serveLoginPage(w http.ResponseWriter, returnURL string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Sign in</title></head>
<body>
<h1>Sign in</h1>
<form method="POST">
<input type="hidden" name="return" value="` + htmlEscape(returnURL) + `">
<label>Username <input name="username" required></label>
<button type="submit">Continue</button>
</form>
</body></html>`))
}
