package oauth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleLaunch issues a single-use SMART EHR launch token for an authenticated client.
func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.launches == nil {
		writeOAuthError(w, http.StatusNotImplemented, "invalid_request", "launch store not configured")
		return
	}
	client, ok := s.requireAuthenticatedClient(w, r)
	if !ok {
		return
	}
	patient := strings.TrimSpace(r.Form.Get("patient"))
	encounter := strings.TrimSpace(r.Form.Get("encounter"))
	user := strings.TrimSpace(r.Form.Get("user"))
	tenant := strings.TrimSpace(r.Form.Get("tenant_hint"))
	if patient == "" && encounter == "" && user == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "patient, encounter, or user is required")
		return
	}
	if tenant == "" {
		tenant = client.TenantHint
	}
	token, err := s.launches.Issue(LaunchContextRecord{
		PatientID:   firstNonEmpty(patient, client.DefaultPatient),
		EncounterID: encounter,
		UserID:      firstNonEmpty(user, client.DefaultUser),
		TenantHint:  tenant,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue launch token")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"launch": token})
}
