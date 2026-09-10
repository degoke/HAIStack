package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	AuthMethodClientSecretPost  = "client_secret_post"
	AuthMethodClientSecretBasic = "client_secret_basic"
	AuthMethodPrivateKeyJWT     = "private_key_jwt"
	AuthMethodNone              = "none"
)

func generateClientSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return randomToken()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashClientSecret(secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func compareClientSecret(storedHash, provided string) bool {
	if strings.TrimSpace(storedHash) == "" {
		return true
	}
	if provided == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(provided)) == nil
}

func normalizeAuthMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return AuthMethodClientSecretBasic
	}
	return method
}

type clientCredentials struct {
	ClientID string
	Secret   string
	Method   string
}

func clientCredentialsFromRequest(r *http.Request, formClientID string) clientCredentials {
	if user, pass, ok := r.BasicAuth(); ok {
		return clientCredentials{
			ClientID: strings.TrimSpace(user),
			Secret:   pass,
			Method:   AuthMethodClientSecretBasic,
		}
	}
	return clientCredentials{
		ClientID: strings.TrimSpace(formClientID),
		Secret:   r.Form.Get("client_secret"),
		Method:   AuthMethodClientSecretPost,
	}
}

func authenticateConfidentialClient(client Client, creds clientCredentials) bool {
	if strings.TrimSpace(client.ClientSecretHash) == "" && strings.TrimSpace(client.ClientSecret) == "" && client.PublicKeyPEM == "" {
		return true
	}
	expected := normalizeAuthMethod(client.TokenEndpointAuthMethod)
	if creds.Method != expected {
		return false
	}
	if creds.ClientID != "" && creds.ClientID != client.ClientID {
		return false
	}
	if client.ClientSecretHash != "" {
		return compareClientSecret(client.ClientSecretHash, creds.Secret)
	}
	if client.ClientSecret != "" {
		return subtle.ConstantTimeCompare([]byte(client.ClientSecret), []byte(creds.Secret)) == 1
	}
	return false
}

// PrepareClientSecret hashes plaintext client secrets before persistence.
func PrepareClientSecret(client *Client) error {
	return prepareClientSecret(client)
}

func prepareClientSecret(client *Client) error {
	if client == nil {
		return nil
	}
	if strings.TrimSpace(client.ClientSecret) == "" {
		return nil
	}
	if client.ClientSecretHash != "" {
		client.ClientSecret = ""
		return nil
	}
	hash, err := hashClientSecret(client.ClientSecret)
	if err != nil {
		return err
	}
	client.ClientSecretHash = hash
	client.ClientSecret = ""
	if client.TokenEndpointAuthMethod == "" {
		client.TokenEndpointAuthMethod = AuthMethodClientSecretBasic
	}
	return nil
}
