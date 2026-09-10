package oauth

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
)

// Handler returns an http.Handler mounting OAuth and SMART metadata routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/launch", s.handleLaunch)
	mux.HandleFunc("/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("/oauth/token", s.handleToken)
	mux.HandleFunc("/oauth/revoke", s.handleRevoke)
	mux.HandleFunc("/oauth/introspect", s.handleIntrospect)
	if s.dynamicClientRegistrationEnabled() {
		mux.HandleFunc("/oauth/register", s.handleRegister)
	}
	mux.HandleFunc("/.well-known/smart-configuration", s.handleSmartConfiguration)
	mux.HandleFunc("/.well-known/openid-configuration", s.handleOpenIDConfiguration)
	if jwks := s.jwksHandler(); jwks != nil {
		mux.HandleFunc("/oauth/jwks", jwks)
		mux.HandleFunc("/.well-known/jwks.json", jwks)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize", "/oauth/token", "/oauth/revoke", "/oauth/introspect", "/oauth/launch", "/oauth/register",
			"/.well-known/smart-configuration", "/.well-known/openid-configuration",
			"/oauth/jwks", "/.well-known/jwks.json":
			mux.ServeHTTP(w, r)
		default:
			writeOAuthError(w, http.StatusNotFound, "not_found", "endpoint not found")
		}
	})
}

func (s *Server) jwksHandler() http.HandlerFunc {
	pub, ok := s.signer.(interface{ PublicJWKS() ([]byte, error) })
	if !ok {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		body, err := pub.PublicJWKS()
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "jwks unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// PublicJWKS returns JWKS JSON for RS256 signers.
func (s RS256Signer) PublicJWKS() ([]byte, error) {
	if s.PrivateKey == nil {
		return nil, ErrInvalidConfig
	}
	n := s.PrivateKey.N
	e := s.PrivateKey.E
	kid := s.Kid
	key := map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"n":   encodeBigInt(n),
		"e":   encodeRSAExponent(e),
	}
	if kid != "" {
		key["kid"] = kid
	}
	doc := map[string]any{"keys": []any{key}}
	return json.Marshal(doc)
}

func encodeBigInt(n *big.Int) string {
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(n.Bytes()), "=")
}

func encodeRSAExponent(e int) string {
	if e == 65537 {
		return "AQAB"
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(big.NewInt(int64(e)).Bytes()), "=")
}
