package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "haistack_oauth_session"
const sessionSecretEnv = "OAUTH_SESSION_SECRET"

// LoginUsersEnv is the production username/password directory.
// Entries are `username:password` or `username:$2a$...` separated by newlines or `;`.
const LoginUsersEnv = "OAUTH_LOGIN_USERS"

// LoginUserStore verifies end-user passwords for session login.
type LoginUserStore interface {
	Verify(username, password string) (UserIdentity, bool)
}

// BcryptLoginUsers is a static username → bcrypt-hash directory.
type BcryptLoginUsers struct {
	hashes map[string]string
}

// HashLoginPassword returns a bcrypt hash of password.
func HashLoginPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// ParsePasswordUsers parses OAUTH_LOGIN_USERS contents.
func ParsePasswordUsers(raw string) (*BcryptLoginUsers, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	users := &BcryptLoginUsers{hashes: make(map[string]string)}
	for _, line := range splitLoginUserEntries(raw) {
		username, secret, ok := strings.Cut(line, ":")
		username = strings.TrimSpace(username)
		secret = strings.TrimSpace(secret)
		if !ok || username == "" || secret == "" {
			return nil, fmt.Errorf("%w: %s entries must be username:password", ErrInvalidConfig, LoginUsersEnv)
		}
		hash := secret
		if !isBcryptHash(secret) {
			hashed, err := HashLoginPassword(secret)
			if err != nil {
				return nil, fmt.Errorf("%w: hash login password for %q: %v", ErrInvalidConfig, username, err)
			}
			hash = hashed
		}
		users.hashes[username] = hash
	}
	if len(users.hashes) == 0 {
		return nil, nil
	}
	return users, nil
}

// PasswordUsersFromEnv loads OAUTH_LOGIN_USERS.
func PasswordUsersFromEnv() (*BcryptLoginUsers, error) {
	return ParsePasswordUsers(os.Getenv(LoginUsersEnv))
}

// RequirePasswordUsersFromEnv requires a non-empty production login directory.
func RequirePasswordUsersFromEnv() (*BcryptLoginUsers, error) {
	users, err := PasswordUsersFromEnv()
	if err != nil {
		return nil, err
	}
	if users == nil || len(users.hashes) == 0 {
		return nil, fmt.Errorf("%w: set %s for production user login", ErrInvalidConfig, LoginUsersEnv)
	}
	return users, nil
}

var (
	dummyLoginHashOnce sync.Once
	dummyLoginHash     []byte
)

func loginDummyHash() []byte {
	dummyLoginHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("timing-dummy"), bcrypt.DefaultCost)
		if err == nil {
			dummyLoginHash = hash
		}
	})
	return dummyLoginHash
}

// Verify implements LoginUserStore.
func (u *BcryptLoginUsers) Verify(username, password string) (UserIdentity, bool) {
	if u == nil {
		return UserIdentity{}, false
	}
	hash, ok := u.hashes[strings.TrimSpace(username)]
	if !ok {
		if dummy := loginDummyHash(); len(dummy) > 0 {
			_ = bcrypt.CompareHashAndPassword(dummy, []byte(password))
		}
		return UserIdentity{}, false
	}
	if strings.TrimSpace(password) == "" {
		return UserIdentity{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return UserIdentity{}, false
	}
	return UserIdentity{Subject: strings.TrimSpace(username)}, true
}

func splitLoginUserEntries(raw string) []string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, ";", "\n")
	var out []string
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func isBcryptHash(secret string) bool {
	return strings.HasPrefix(secret, "$2a$") || strings.HasPrefix(secret, "$2b$") || strings.HasPrefix(secret, "$2y$")
}

// SessionUserAuthenticator authenticates users from signed session cookies.
type SessionUserAuthenticator struct {
	secret     []byte
	now        func() time.Time
	ttl        time.Duration
	issuer     string
	cookiePath string
	users      LoginUserStore
}

// SessionAuthConfig configures signed cookie sessions for production consent login.
type SessionAuthConfig struct {
	Secret     string
	TTL        time.Duration
	Now        func() time.Time
	Issuer     string
	CookiePath string
	Users      LoginUserStore
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
	cookiePath := strings.TrimSpace(cfg.CookiePath)
	if cookiePath == "" {
		cookiePath = "/"
	}
	return &SessionUserAuthenticator{
		secret:     []byte(secret),
		now:        now,
		ttl:        ttl,
		issuer:     NormalizeIssuerURL(cfg.Issuer),
		cookiePath: cookiePath,
		users:      cfg.Users,
	}, nil
}

// ForIssuer returns a copy bound to issuer and cookie path (tenant isolation).
func (a *SessionUserAuthenticator) ForIssuer(issuer, cookiePath string) *SessionUserAuthenticator {
	if a == nil {
		return nil
	}
	cp := *a
	cp.issuer = NormalizeIssuerURL(issuer)
	if strings.TrimSpace(cookiePath) != "" {
		cp.cookiePath = cookiePath
	}
	return &cp
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
		Path:     a.cookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		Expires:  a.now().Add(a.ttl),
	})
	return nil
}

type sessionPayload struct {
	Subject string `json:"sub"`
	Issuer  string `json:"iss,omitempty"`
	Exp     int64  `json:"exp"`
}

func (a *SessionUserAuthenticator) signSession(subject string) (string, error) {
	payload := sessionPayload{
		Subject: strings.TrimSpace(subject),
		Issuer:  a.issuer,
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
	if a.issuer != "" && !EntryIssuerMatches(payload.Issuer, a.issuer) {
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
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if formReturn := strings.TrimSpace(r.FormValue("return")); formReturn != "" {
			returnURL = formReturn
		}
	}
	safeReturn, err := safeLoginReturnURL(s.cfg.Issuer, returnURL)
	if err != nil {
		http.Error(w, "invalid return url", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		if _, loggedIn := s.authenticatedUser(r); loggedIn {
			http.Redirect(w, r, safeReturn, http.StatusFound)
			return
		}
		serveLoginPage(w, safeReturn)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	if auth.users == nil {
		http.Error(w, "login unavailable", http.StatusServiceUnavailable)
		return
	}
	subject := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if !s.rateLimitLogin(w, r, subject) {
		return
	}
	if subject == "" || password == "" {
		http.Error(w, "username and password required", http.StatusBadRequest)
		return
	}
	ident, ok := auth.users.Verify(subject, password)
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := auth.SetSession(w, ident.Subject); err != nil {
		http.Error(w, "session unavailable", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, safeReturn, http.StatusFound)
}

func safeLoginReturnURL(issuer, raw string) (string, error) {
	issuer = NormalizeIssuerURL(issuer)
	if issuer == "" {
		return "", fmt.Errorf("oauth: issuer required")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return issuer + "/oauth/authorize", nil
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		raw = issuer + raw
	}
	iu, err := url.Parse(issuer)
	if err != nil || iu.Scheme == "" || iu.Host == "" {
		return "", fmt.Errorf("oauth: invalid issuer")
	}
	ru, err := url.Parse(raw)
	if err != nil || ru.Scheme == "" || ru.Host == "" {
		return "", fmt.Errorf("oauth: invalid return url")
	}
	if ru.User != nil {
		return "", fmt.Errorf("oauth: invalid return url")
	}
	if !strings.EqualFold(ru.Scheme, iu.Scheme) || !strings.EqualFold(ru.Host, iu.Host) {
		return "", fmt.Errorf("oauth: return url must match issuer origin")
	}
	issuerPath := strings.TrimSuffix(iu.Path, "/")
	reqPath := ru.EscapedPath()
	if reqPath == "" {
		reqPath = ru.Path
	}
	if issuerPath != "" {
		if reqPath != issuerPath && !strings.HasPrefix(reqPath, issuerPath+"/") {
			return "", fmt.Errorf("oauth: return url must stay under issuer path")
		}
	} else if !strings.HasPrefix(reqPath, "/oauth") && !strings.HasPrefix(reqPath, "/.well-known") {
		return "", fmt.Errorf("oauth: return url path not allowed")
	}
	return ru.String(), nil
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
<label>Password <input name="password" type="password" required></label>
<button type="submit">Continue</button>
</form>
</body></html>`))
}
