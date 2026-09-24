package ai

import "strings"

// fieldMatchesAllowedPolicy reports whether a requested field or patch path is
// allowed by policy. An allowed parent permits descendants (e.g. "name" allows
// "name[0].family"); matching is prefix-safe (does not allow "nameplate").
func fieldMatchesAllowedPolicy(field string, allowed []string) bool {
	field = strings.TrimSpace(field)
	for _, a := range allowed {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if field == a {
			return true
		}
		if strings.HasPrefix(field, a+".") || strings.HasPrefix(field, a+"[") {
			return true
		}
	}
	return false
}
