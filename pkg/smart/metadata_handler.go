package smart

import (
	"encoding/json"
	"net/http"
)

// WellKnownHandler serves /.well-known/smart-configuration for reference hosts.
func WellKnownHandler(cfg Configuration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	})
}
