package jobs

import (
	"encoding/json"
	"fmt"
)

// JobOwner records the principal that enqueued a background job.
type JobOwner struct {
	PrincipalID string `json:"principalId,omitempty"`
	TenantID    string `json:"tenantId,omitempty"`
}

const ownerPayloadKey = "_owner"

// StampOwner records the requesting principal on a job payload envelope.
func StampOwner(payload []byte, owner JobOwner) ([]byte, error) {
	if owner.PrincipalID == "" && owner.TenantID == "" {
		return payload, nil
	}
	meta := map[string]any{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &meta); err != nil {
			return nil, fmt.Errorf("decode job payload: %w", err)
		}
	}
	meta[ownerPayloadKey] = owner
	return json.Marshal(meta)
}

// OwnerFromPayload returns the recorded job owner, if present.
func OwnerFromPayload(payload []byte) (JobOwner, bool) {
	if len(payload) == 0 {
		return JobOwner{}, false
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(payload, &meta); err != nil {
		return JobOwner{}, false
	}
	raw, ok := meta[ownerPayloadKey]
	if !ok || len(raw) == 0 {
		return JobOwner{}, false
	}
	var owner JobOwner
	if err := json.Unmarshal(raw, &owner); err != nil {
		return JobOwner{}, false
	}
	if owner.PrincipalID == "" && owner.TenantID == "" {
		return JobOwner{}, false
	}
	return owner, true
}
