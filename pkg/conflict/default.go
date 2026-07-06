package conflict

// DefaultRulePack returns the built-in v1 strict safe-list policy.
func DefaultRulePack() RulePack {
	return RulePack{
		Name: "haistack-conflict/default-v1",
		Rules: []Rule{
			// Safe auto-merge candidates.
			{ResourceType: "Patient", PathPrefix: "Patient.telecom", Semantics: RuleSemanticsAutoMerge, Description: "Patient contact points"},
			{ResourceType: "Patient", PathPrefix: "Patient.address", Semantics: RuleSemanticsAutoMerge, Description: "Patient addresses"},
			{ResourceType: "Appointment", PathPrefix: "Appointment.note", Semantics: RuleSemanticsAppendOnly, Description: "Appointment notes append"},
			{ResourceType: "Encounter", PathPrefix: "Encounter.statusHistory", Semantics: RuleSemanticsAppendOnly, Description: "Encounter status history append"},

			// Human-review defaults.
			{ResourceType: "MedicationRequest", PathPrefix: "MedicationRequest.dosageInstruction", Semantics: RuleSemanticsReviewOnly, Description: "Medication dosage instruction"},
			{ResourceType: "AllergyIntolerance", PathPrefix: "AllergyIntolerance.clinicalStatus", Semantics: RuleSemanticsReviewOnly, Description: "Allergy clinical status"},
			{ResourceType: "Observation", PathPrefix: "Observation.value", Semantics: RuleSemanticsReviewOnly, Description: "Observation value"},
			{ResourceType: "Consent", PathPrefix: "Consent.provision", Semantics: RuleSemanticsReviewOnly, Description: "Consent provision"},
			{ResourceType: "Appointment", PathPrefix: "Appointment.start", Semantics: RuleSemanticsReviewOnly, Description: "Appointment start time"},
			{ResourceType: "Patient", PathPrefix: "Patient.birthDate", Semantics: RuleSemanticsReviewOnly, Description: "Patient birth date"},
		},
	}
}
