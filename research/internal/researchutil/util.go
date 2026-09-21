// Package researchutil holds small helpers shared by HAIStack research runners.
package researchutil

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// FixedTime is the deterministic timestamp used by research artefacts.
func FixedTime() time.Time {
	return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
}

// HashBytes returns a lowercase SHA-256 hex digest.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashJSON canonicalizes v as JSON and hashes the bytes.
func HashJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return HashBytes(data), nil
}

// ParseResource parses FHIR JSON into a ResourceEnvelope.
func ParseResource(resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	env, err := types.NewJSONCodec().ParseJSON(resourceType, data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", resourceType, err)
	}
	return env, nil
}

// MustJSON marshals v as compact JSON or panics.
func MustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// RepoRoot walks up from the working directory until it finds go.mod.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
