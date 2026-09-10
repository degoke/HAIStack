package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// SMARTClient supports SMART discovery, auth-code+PKCE, and backend client assertion flows.
type SMARTClient struct {
	client *Client
}

// SMARTConfiguration is parsed from .well-known/smart-configuration.
type SMARTConfiguration struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	RegistrationEndpoint   string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint     string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported    []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethods   []string `json:"code_challenge_methods_supported,omitempty"`
	Raw                    []byte   `json:"-"`
}

// TokenResponse is a parsed OAuth2 token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Patient      string `json:"patient,omitempty"`
	Encounter    string `json:"encounter,omitempty"`
	Raw          []byte `json:"-"`
}

// PKCEChallenge holds a PKCE verifier/challenge pair.
type PKCEChallenge struct {
	Verifier  string
	Challenge string
	Method    string
}

// AuthCodeRequest builds an authorization URL for the auth-code+PKCE flow.
type AuthCodeRequest struct {
	Config      *SMARTConfiguration
	ClientID    string
	RedirectURI string
	Scope       string
	State       string
	PKCE        *PKCEChallenge
	Launch      string
	Aud         string
	ExtraParams map[string]string
}

// ClientAssertionRequest builds a backend-service token exchange request.
type ClientAssertionRequest struct {
	TokenEndpoint string
	ClientID      string
	Scope         string
	PrivateKey    *rsa.PrivateKey
	KeyID         string
	Algorithm     string
	Audience      string
	Expiry        time.Duration
}

// Discover fetches SMART configuration from the issuer.
func (s *SMARTClient) Discover(ctx context.Context, issuer string) (*SMARTConfiguration, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("smart client is nil")
	}
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	u := issuer + "/.well-known/smart-configuration"
	raw, err := s.client.do(ctx, requestOptions{
		method:   "GET",
		url:      u,
		accept:   "application/json",
		skipAuth: true,
	})
	if err != nil {
		return nil, err
	}
	var cfg SMARTConfiguration
	if err := json.Unmarshal(raw.Body, &cfg); err != nil {
		return nil, err
	}
	cfg.Raw = append([]byte(nil), raw.Body...)
	if cfg.Issuer == "" {
		cfg.Issuer = issuer
	}
	return &cfg, nil
}

// NewPKCEChallenge generates an S256 PKCE challenge.
func NewPKCEChallenge() (*PKCEChallenge, error) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, err
	}
	return &PKCEChallenge{
		Verifier:  verifier,
		Challenge: codeChallengeS256(verifier),
		Method:    "S256",
	}, nil
}

// BuildAuthURL constructs the authorization endpoint URL for auth-code+PKCE.
func (s *SMARTClient) BuildAuthURL(req AuthCodeRequest) (string, error) {
	if req.Config == nil || req.Config.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("authorization endpoint is required")
	}
	if req.ClientID == "" {
		return "", fmt.Errorf("clientId is required")
	}
	if req.RedirectURI == "" {
		return "", fmt.Errorf("redirectUri is required")
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", req.ClientID)
	values.Set("redirect_uri", req.RedirectURI)
	if req.Scope != "" {
		values.Set("scope", req.Scope)
	}
	if req.State != "" {
		values.Set("state", req.State)
	}
	if req.PKCE != nil {
		values.Set("code_challenge", req.PKCE.Challenge)
		values.Set("code_challenge_method", req.PKCE.Method)
	}
	if req.Launch != "" {
		values.Set("launch", req.Launch)
	}
	if req.Aud != "" {
		values.Set("aud", req.Aud)
	}
	for k, v := range req.ExtraParams {
		values.Set(k, v)
	}
	return req.Config.AuthorizationEndpoint + "?" + values.Encode(), nil
}

const (
	ClientAuthSecretPost    = "client_secret_post"
	ClientAuthSecretBasic   = "client_secret_basic"
	ClientAuthPrivateKeyJWT = "private_key_jwt"
)

// ClientJWTAuth configures private_key_jwt authentication at the token endpoint.
type ClientJWTAuth struct {
	PrivateKey *rsa.PrivateKey
	KeyID      string
	Algorithm  string
	Expiry     time.Duration
}

// AuthCodeExchangeRequest exchanges an authorization code for tokens.
type AuthCodeExchangeRequest struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	ClientAuth    string
	ClientJWT     *ClientJWTAuth
	RedirectURI   string
	Code          string
	PKCE          *PKCEChallenge
}

// ExchangeAuthCode exchanges an authorization code for tokens.
func (s *SMARTClient) ExchangeAuthCode(ctx context.Context, req AuthCodeExchangeRequest) (*TokenResponse, error) {
	if req.TokenEndpoint == "" {
		return nil, fmt.Errorf("token endpoint is required")
	}
	if req.ClientID == "" {
		return nil, fmt.Errorf("clientId is required")
	}
	if req.RedirectURI == "" {
		return nil, fmt.Errorf("redirectUri is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", req.Code)
	values.Set("redirect_uri", req.RedirectURI)
	basicUser, basicPass, err := applyTokenEndpointClientAuth(values, req.TokenEndpoint, req.ClientID, req.ClientSecret, req.ClientAuth, req.ClientJWT)
	if err != nil {
		return nil, err
	}
	if req.PKCE != nil {
		values.Set("code_verifier", req.PKCE.Verifier)
	}
	return s.postToken(ctx, req.TokenEndpoint, values, basicUser, basicPass)
}

// ExchangeClientAssertion exchanges a backend-service client assertion for tokens.
func (s *SMARTClient) ExchangeClientAssertion(ctx context.Context, req ClientAssertionRequest) (*TokenResponse, error) {
	if req.TokenEndpoint == "" {
		return nil, fmt.Errorf("token endpoint is required")
	}
	if req.ClientID == "" {
		return nil, fmt.Errorf("clientId is required")
	}
	if req.PrivateKey == nil {
		return nil, fmt.Errorf("private key is required")
	}
	audience := req.Audience
	if audience == "" {
		audience = req.TokenEndpoint
	}
	expiry := req.Expiry
	if expiry <= 0 {
		expiry = 5 * time.Minute
	}
	assertion, err := generateClientAssertion(req.ClientID, audience, req.PrivateKey, req.KeyID, req.Algorithm, expiry)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	values.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	values.Set("client_assertion", assertion)
	if req.Scope != "" {
		values.Set("scope", req.Scope)
	}
	return s.postToken(ctx, req.TokenEndpoint, values, "", "")
}

// RefreshTokenRequest refreshes an access token when the server supports it.
type RefreshTokenRequest struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	ClientAuth    string
	ClientJWT     *ClientJWTAuth
	RefreshToken  string
}

// RevokeTokenRequest revokes an access or refresh token when the server supports it.
type RevokeTokenRequest struct {
	RevocationEndpoint string
	ClientID           string
	ClientSecret       string
	ClientAuth         string
	ClientJWT          *ClientJWTAuth
	Token              string
	TokenTypeHint      string
}

// RefreshToken refreshes an access token when the server supports it.
func (s *SMARTClient) RefreshToken(ctx context.Context, req RefreshTokenRequest) (*TokenResponse, error) {
	if req.TokenEndpoint == "" {
		return nil, fmt.Errorf("token endpoint is required")
	}
	if req.ClientID == "" {
		return nil, fmt.Errorf("clientId is required")
	}
	if req.RefreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", req.RefreshToken)
	basicUser, basicPass, err := applyTokenEndpointClientAuth(values, req.TokenEndpoint, req.ClientID, req.ClientSecret, req.ClientAuth, req.ClientJWT)
	if err != nil {
		return nil, err
	}
	return s.postToken(ctx, req.TokenEndpoint, values, basicUser, basicPass)
}

// RevokeToken revokes an access or refresh token when the server supports it.
func (s *SMARTClient) RevokeToken(ctx context.Context, req RevokeTokenRequest) error {
	if req.RevocationEndpoint == "" {
		return fmt.Errorf("revocation endpoint is required")
	}
	if req.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}
	if req.Token == "" {
		return fmt.Errorf("token is required")
	}
	values := url.Values{}
	values.Set("token", req.Token)
	if req.TokenTypeHint != "" {
		values.Set("token_type_hint", req.TokenTypeHint)
	}
	basicUser, basicPass, err := applyTokenEndpointClientAuth(values, req.RevocationEndpoint, req.ClientID, req.ClientSecret, req.ClientAuth, req.ClientJWT)
	if err != nil {
		return err
	}
	return s.postRevoke(ctx, req.RevocationEndpoint, values, basicUser, basicPass)
}

func applyTokenEndpointClientAuth(values url.Values, tokenEndpoint, clientID, clientSecret, clientAuth string, jwtAuth *ClientJWTAuth) (string, string, error) {
	auth := clientAuth
	if auth == "" {
		if jwtAuth != nil && jwtAuth.PrivateKey != nil {
			auth = ClientAuthPrivateKeyJWT
		} else {
			auth = ClientAuthSecretPost
		}
	}
	switch auth {
	case ClientAuthPrivateKeyJWT:
		if jwtAuth == nil || jwtAuth.PrivateKey == nil {
			return "", "", fmt.Errorf("private key is required for private_key_jwt")
		}
		expiry := jwtAuth.Expiry
		if expiry <= 0 {
			expiry = 5 * time.Minute
		}
		assertion, err := generateClientAssertion(clientID, tokenEndpoint, jwtAuth.PrivateKey, jwtAuth.KeyID, jwtAuth.Algorithm, expiry)
		if err != nil {
			return "", "", err
		}
		values.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		values.Set("client_assertion", assertion)
		return "", "", nil
	case ClientAuthSecretPost:
		values.Set("client_id", clientID)
		if clientSecret != "" {
			values.Set("client_secret", clientSecret)
		}
		user, pass := tokenClientAuth(clientID, clientSecret, auth)
		return user, pass, nil
	case ClientAuthSecretBasic:
		user, pass := tokenClientAuth(clientID, clientSecret, auth)
		return user, pass, nil
	default:
		return "", "", fmt.Errorf("unsupported client auth method %q", auth)
	}
}

func tokenClientAuth(clientID, secret, method string) (string, string) {
	if method == ClientAuthSecretBasic && clientID != "" && secret != "" {
		return clientID, secret
	}
	return "", ""
}

// TokenProviderFromResponse returns a TokenProvider backed by a token response.
func TokenProviderFromResponse(resp *TokenResponse) TokenProvider {
	return StaticTokenProvider{Token: resp.AccessToken}
}

// ParseTokenClaims parses token claims without verification (caller validates separately).
func ParseTokenClaims(token string) (smart.TokenClaims, error) {
	return smart.ParseTokenUnverified(token)
}

func (s *SMARTClient) postRevoke(ctx context.Context, revocationEndpoint string, values url.Values, basicUser, basicPass string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("smart client is nil")
	}
	_, err := s.client.do(ctx, requestOptions{
		method:      "POST",
		url:         revocationEndpoint,
		body:        []byte(values.Encode()),
		contentType: "application/x-www-form-urlencoded",
		skipAuth:    true,
		basicUser:   basicUser,
		basicPass:   basicPass,
	})
	return err
}

func (s *SMARTClient) postToken(ctx context.Context, tokenEndpoint string, values url.Values, basicUser, basicPass string) (*TokenResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("smart client is nil")
	}
	raw, err := s.client.do(ctx, requestOptions{
		method:      "POST",
		url:         tokenEndpoint,
		body:        []byte(values.Encode()),
		contentType: "application/x-www-form-urlencoded",
		accept:      "application/json",
		basicUser:   basicUser,
		basicPass:   basicPass,
		skipAuth:    true,
	})
	if err != nil {
		return nil, err
	}
	var tr TokenResponse
	if err := json.Unmarshal(raw.Body, &tr); err != nil {
		return nil, err
	}
	tr.Raw = append([]byte(nil), raw.Body...)
	return &tr, nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64URLEncode(b), nil
}

func codeChallengeS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64URLEncode(h[:])
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func generateClientAssertion(clientID, audience string, key *rsa.PrivateKey, keyID, algorithm string, expiry time.Duration) (string, error) {
	if algorithm == "" {
		algorithm = "RS256"
	}
	now := time.Now()
	header := map[string]string{
		"alg": algorithm,
		"typ": "JWT",
	}
	if keyID != "" {
		header["kid"] = keyID
	}
	payload := map[string]interface{}{
		"iss": clientID,
		"sub": clientID,
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(expiry).Unix(),
		"jti": fmt.Sprintf("%d", now.UnixNano()),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	unsigned := base64URLEncode(headerJSON) + "." + base64URLEncode(payloadJSON)
	sig, err := signJWT(unsigned, key, algorithm)
	if err != nil {
		return "", err
	}
	return unsigned + "." + sig, nil
}

func signJWT(unsigned string, key *rsa.PrivateKey, algorithm string) (string, error) {
	hash := sha256.Sum256([]byte(unsigned))
	var sig []byte
	var err error
	switch algorithm {
	case "RS256", "":
		sig, err = rsa.SignPKCS1v15(rand.Reader, key, 0, hash[:])
	default:
		return "", fmt.Errorf("unsupported algorithm %q", algorithm)
	}
	if err != nil {
		return "", err
	}
	return base64URLEncode(sig), nil
}
