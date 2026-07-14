package binary

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// HashSHA256 returns the lowercase hex SHA-256 digest of data.
func HashSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashReaderSHA256 streams data through SHA-256 and returns the hex digest.
func HashReaderSHA256(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
