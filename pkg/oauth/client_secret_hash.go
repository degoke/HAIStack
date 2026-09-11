package oauth

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func hashClientSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// HashClientSecret returns a bcrypt hash suitable for persistent client secret storage.
func HashClientSecret(secret string) (string, error) {
	return hashClientSecret(secret)
}

func verifyStoredClientSecret(stored, provided string) bool {
	if stored == "" {
		return provided == ""
	}
	if isBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(provided)) == nil
	}
	return secretEqual(provided, stored)
}

func isBcryptHash(value string) bool {
	return strings.HasPrefix(value, "$2a$") || strings.HasPrefix(value, "$2b$") || strings.HasPrefix(value, "$2y$")
}
