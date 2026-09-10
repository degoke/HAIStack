package client

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/smart"
)

// ScopeSet is a validated SMART scope set for outbound authorize/token requests.
type ScopeSet = smart.ScopeSet

// ParseScopes validates and normalizes SMART 1.x and 2.2 scope strings.
func ParseScopes(raw string) (ScopeSet, error) {
	return smart.ParseScopes(raw)
}

// ScopeBuilder constructs SMART 2.2 CRUDS scope strings.
type ScopeBuilder struct {
	Actor    smart.ActorClass
	Resource string
	Letters  string
	Filters  url.Values
}

// ReadSearch returns a patient/Observation.rs style scope.
func (b ScopeBuilder) String() string {
	raw := fmt.Sprintf("%s/%s.%s", b.Actor, b.Resource, b.Letters)
	if len(b.Filters) > 0 {
		raw += "?" + b.Filters.Encode()
	}
	return raw
}

// PatientObservationFiltered builds patient/Observation.rs?param=value.
func PatientObservationFiltered(category string) (string, error) {
	set, err := ParseScopes(fmt.Sprintf("patient/Observation.rs?category=%s", category))
	if err != nil {
		return "", err
	}
	return set.SpaceSeparated(), nil
}

// JoinScopes joins validated scope strings.
func JoinScopes(scopes ...string) (string, error) {
	var parts []string
	for _, raw := range scopes {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		set, err := ParseScopes(raw)
		if err != nil {
			return "", err
		}
		parts = append(parts, set.Strings()...)
	}
	return strings.Join(parts, " "), nil
}
