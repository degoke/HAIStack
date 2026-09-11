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
	mux.HandleFunc("/oauth/token", s.wrapTokenRateLimit(s.handleToken))
	mux.HandleFunc("/oauth/revoke", s.handleRevoke)
	mux.HandleFunc("/oauth/introspect", s.handleIntrospect)
	if s.dynamicClientRegistrationEnabled() {
		mux.HandleFunc("/oauth/register", s.wrapRegisterRateLimit(s.handleRegister))
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

func (s *Server) wrapTokenRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimitOAuth(w, r, s.tokenRateLimiter()) {
			return
		}
		next(w, r)
	}
}

func (s *Server) wrapRegisterRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimitOAuth(w, r, s.registerRateLimiter()) {
			return
		}
		next(w, r)
	}
}

func (s *Server) jwksHandler() http.HandlerFunc {
	signers := s.jwksSigners
	if len(signers) == 0 && s.signer != nil {
		signers = []TokenSigner{s.signer}
	}
	if len(signers) == 0 {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		body, err := MarshalJWKS(signers)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "jwks unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// PublicJWKS returns JWKS JSON for a single RS256 signer.
func (s RS256Signer) PublicJWKS() ([]byte, error) {
	key, err := rs256PublicJWK(s)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"keys": []any{key}})
}

func rs256PublicJWK(s RS256Signer) (map[string]any, error) {
	if s.PrivateKey == nil {
		return nil, ErrInvalidConfig
	}
	key := map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"n":   encodeBigInt(s.PrivateKey.N),
		"e":   encodeRSAExponent(s.PrivateKey.E),
	}
	if s.Kid != "" {
		key["kid"] = s.Kid
	}
	return key, nil
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

// MarshalJWKS returns JWKS JSON for the given signers.
func MarshalJWKS(signers []TokenSigner) ([]byte, error) {
	keys := make([]any, 0, len(signers))
	seen := make(map[string]struct{})
	for _, signer := range signers {
		if rs, ok := signer.(RS256Signer); ok {
			key, err := rs256PublicJWK(rs)
			if err != nil {
				return nil, err
			}
			kid, _ := key["kid"].(string)
			if kid != "" {
				if _, ok := seen[kid]; ok {
					continue
				}
				seen[kid] = struct{}{}
			}
			keys = append(keys, key)
			continue
		}
		pub, ok := signer.(interface{ PublicJWKS() ([]byte, error) })
		if !ok {
			continue
		}
		body, err := pub.PublicJWKS()
		if err != nil {
			return nil, err
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		rawKeys, _ := doc["keys"].([]any)
		for _, raw := range rawKeys {
			keyDoc, _ := raw.(map[string]any)
			kid, _ := keyDoc["kid"].(string)
			if kid != "" {
				if _, ok := seen[kid]; ok {
					continue
				}
				seen[kid] = struct{}{}
			}
			keys = append(keys, keyDoc)
		}
	}
	return json.Marshal(map[string]any{"keys": keys})
}
