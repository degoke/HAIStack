package registry

import "sort"

var patientSearchParamPriority = []string{
	"patient",
	"subject",
	"individual",
	"beneficiary",
}

// PatientSearchParameterCode returns the FHIR search parameter code that scopes
// resourceType to one Patient, derived from installed reference SearchParameters.
// Resource-specific parameters are preferred over cross-resource definitions such
// as clinical-patient so the chosen code matches indexed search fields.
func (s *Snapshot) PatientSearchParameterCode(resourceType string) (string, bool) {
	if s == nil || resourceType == "" || resourceType == "Patient" {
		return "", false
	}
	params := s.SearchParametersFor(resourceType)
	candidates := make([]SearchParameterInfo, 0, len(params))
	for _, param := range params {
		if param.Type != "reference" {
			continue
		}
		if param.Code == "" || param.Code[0] == '_' {
			continue
		}
		if !searchParameterTargetsPatient(param.Target) {
			continue
		}
		candidates = append(candidates, param)
	}
	if len(candidates) == 0 {
		return "", false
	}
	if code, ok := selectPatientSearchParamCode(candidates, true); ok {
		return code, true
	}
	return selectPatientSearchParamCode(candidates, false)
}

func selectPatientSearchParamCode(candidates []SearchParameterInfo, dedicatedOnly bool) (string, bool) {
	filtered := candidates
	if dedicatedOnly {
		filtered = make([]SearchParameterInfo, 0, len(candidates))
		for _, param := range candidates {
			if param.BaseCount == 1 {
				filtered = append(filtered, param)
			}
		}
		if len(filtered) == 0 {
			return "", false
		}
	}
	for _, code := range patientSearchParamPriority {
		for _, param := range filtered {
			if param.Code == code {
				return param.Code, true
			}
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left := patientSearchParamRank(filtered[i])
		right := patientSearchParamRank(filtered[j])
		if left != right {
			return left > right
		}
		return filtered[i].Code < filtered[j].Code
	})
	return filtered[0].Code, true
}

func searchParameterTargetsPatient(targets []string) bool {
	if len(targets) == 0 {
		return true
	}
	for _, target := range targets {
		if target == "Patient" {
			return true
		}
	}
	return false
}

func patientSearchParamRank(param SearchParameterInfo) int {
	score := 0
	if param.BaseCount == 1 {
		score += 200
	}
	if len(param.Target) == 1 && param.Target[0] == "Patient" {
		score += 100
	} else if searchParameterTargetsPatient(param.Target) {
		score += 50
	}
	return score
}
