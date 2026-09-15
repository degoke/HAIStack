package oauth

import "net/http"

// UserIdentity is an authenticated end-user at the authorization server.
type UserIdentity struct {
	Subject  string
	FHIRUser string
}

// UserAuthenticator identifies the human user approving SMART authorization.
type UserAuthenticator interface {
	AuthenticateUser(r *http.Request) (UserIdentity, bool)
}

// UserAuthenticatorFunc adapts a function to UserAuthenticator.
type UserAuthenticatorFunc func(r *http.Request) (UserIdentity, bool)

func (f UserAuthenticatorFunc) AuthenticateUser(r *http.Request) (UserIdentity, bool) {
	if f == nil {
		return UserIdentity{}, false
	}
	return f(r)
}

// StaticUserAuthenticator returns a fixed user for demos and tests.
func StaticUserAuthenticator(user UserIdentity) UserAuthenticator {
	return UserAuthenticatorFunc(func(*http.Request) (UserIdentity, bool) {
		if user.Subject == "" {
			return UserIdentity{}, false
		}
		return user, true
	})
}
