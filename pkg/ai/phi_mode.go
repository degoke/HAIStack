package ai

// PHIMode controls how aggressively StructureDefinition elements become paths.
type PHIMode string

const (
	// PHIModeCatalog uses PHICatalog paths only (no SD-derived paths).
	PHIModeCatalog PHIMode = "catalog"
	// PHIModeStandard uses catalog plus SD complex types and gated primitives.
	PHIModeStandard PHIMode = "standard"
	// PHIModeFull uses catalog plus all SD-sensitive elements (including every string leaf).
	PHIModeFull PHIMode = "full"
)

// EvalMode controls FHIRPath compile/eval during scrubbing.
type EvalMode string

const (
	// EvalModeNever skips FHIRPath evaluation (segment + catalog walk only).
	EvalModeNever EvalMode = "never"
	// EvalModeKeywordsOnly evaluates expressions without a segment equivalent (e.g. text.`div`).
	EvalModeKeywordsOnly EvalMode = "keywords-only"
	// EvalModeAlways evaluates all compiled expressions.
	EvalModeAlways EvalMode = "always"
)

// DefaultWarmResourceTypes are pre-warmed on Executor startup when profiles are configured.
var DefaultWarmResourceTypes = []string{
	"Patient", "Observation", "Encounter", "Appointment", "Practitioner", "Condition",
}

// StructureRulesForMode returns PHIStructureRules for the given mode.
func StructureRulesForMode(mode PHIMode, base PHIStructureRules) PHIStructureRules {
	switch mode {
	case PHIModeCatalog:
		return PHIStructureRules{
			SensitiveTypeCodes:           map[string]bool{},
			SensitiveExtensionURLs:       base.SensitiveExtensionURLs,
			SensitiveExtensionSubstrings: base.SensitiveExtensionSubstrings,
			RedactMustSupportPrimitives:  false,
			RedactIsSummaryPrimitives:    false,
		}
	case PHIModeFull:
		if base.SensitiveTypeCodes == nil {
			base.SensitiveTypeCodes = defaultSensitiveFHIRTypes()
		}
		return base
	default: // standard
		return standardPHIStructureRules(base)
	}
}

func standardPHIStructureRules(base PHIStructureRules) PHIStructureRules {
	codes := map[string]bool{
		"HumanName": true, "Address": true, "ContactPoint": true, "Identifier": true,
		"Attachment": true, "Reference": true, "Period": true,
		"markdown": true, "uri": true, "url": true,
		"date": true, "dateTime": true, "instant": true, "time": true,
		"base64Binary": true,
	}
	return PHIStructureRules{
		SensitiveTypeCodes:           codes,
		SensitiveExtensionURLs:       base.SensitiveExtensionURLs,
		SensitiveExtensionSubstrings: base.SensitiveExtensionSubstrings,
		RedactMustSupportPrimitives:  base.RedactMustSupportPrimitives,
		RedactIsSummaryPrimitives:    base.RedactIsSummaryPrimitives,
	}
}
