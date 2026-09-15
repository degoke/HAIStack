package authztest

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
}

func unsignedTestJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + ".AA"
}
