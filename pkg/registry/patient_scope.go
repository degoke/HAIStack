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
	for _, code := range patientSearchParamPriority {
		for _, param := range candidates {
			if param.Code == code {
				return param.Code, true
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := patientSearchParamRank(candidates[i])
		right := patientSearchParamRank(candidates[j])
		if left != right {
			return left > right
		}
		return candidates[i].Code < candidates[j].Code
	})
	return candidates[0].Code, true
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
	if len(param.Target) == 1 && param.Target[0] == "Patient" {
		return 100
	}
	if searchParameterTargetsPatient(param.Target) {
		return 50
	}
	return 0
}
