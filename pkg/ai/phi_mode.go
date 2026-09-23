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

// EvalMode controls which FHIRPath expression strings augment segment redaction.
// Scrubbing does not evaluate FHIRPath against live values (only path segments
// derived from expression text). Search tool output skips label augmentation
// unless EvalModeAlways is set (see effectiveEvalMode).
type EvalMode string

const (
	// EvalModeNever uses segment and catalog paths only (no label augmentation).
	EvalModeNever EvalMode = "never"
	// EvalModeKeywordsOnly adds segment paths from expressions without an indexed
	// equivalent (e.g. text.`div`).
	EvalModeKeywordsOnly EvalMode = "keywords-only"
	// EvalModeAlways adds segment paths from every compileable expression string,
	// including on search tool output.
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
