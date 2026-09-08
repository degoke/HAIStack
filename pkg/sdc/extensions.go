package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	FHIRBaseURL = "http://hl7.org/fhir/StructureDefinition/"

	MinLengthExtension                 = FHIRBaseURL + "minLength"
	QuestionnaireMinValueExtension     = FHIRBaseURL + "questionnaire-minValue"
	QuestionnaireMaxValueExtension     = FHIRBaseURL + "questionnaire-maxValue"
	QuestionnaireMinOccursExtension    = FHIRBaseURL + "questionnaire-minOccurs"
	QuestionnaireMaxOccursExtension    = FHIRBaseURL + "questionnaire-maxOccurs"
	QuestionnaireReferenceResourceExt  = FHIRBaseURL + "questionnaire-referenceResource"
	QuestionnaireReferenceProfileExt   = FHIRBaseURL + "questionnaire-referenceProfile"
	QuestionnaireReferenceFilterExt    = FHIRBaseURL + "questionnaire-referenceFilter"
	QuestionnaireUnitExtension         = FHIRBaseURL + "questionnaire-unit"
	QuestionnaireUnitOptionExtension   = FHIRBaseURL + "questionnaire-unitOption"
	QuestionnaireUnitValueSetExtension = FHIRBaseURL + "questionnaire-unitValueSet"
	QuestionnaireChoiceOrientationExt  = FHIRBaseURL + "questionnaire-choiceOrientation"
	QuestionnaireOptionExclusiveExt    = FHIRBaseURL + "questionnaire-optionExclusive"
	QuestionnaireSliderStepValueExt    = FHIRBaseURL + "questionnaire-sliderStepValue"
	QuestionnaireUsageModeExtension    = FHIRBaseURL + "questionnaire-usageMode"
	QuestionnaireDisplayCategoryExt    = FHIRBaseURL + "questionnaire-displayCategory"
	QuestionnaireSupportLinkExtension  = FHIRBaseURL + "questionnaire-supportLink"
	QuestionnaireFHIRTypeExtension     = FHIRBaseURL + "questionnaire-fhirType"
	QuestionnaireBaseTypeExtension     = FHIRBaseURL + "questionnaire-baseType"
	QuestionnaireOptionPrefixExtension = FHIRBaseURL + "questionnaire-optionPrefix"
	QuestionnaireRegexExtension        = FHIRBaseURL + "regex"
	QuestionnaireConstraintExtension   = FHIRBaseURL + "questionnaire-constraint"

	SDCLaunchContextExtension      = SDCBaseURL + "sdc-questionnaire-launchContext"
	SDCVariableExtension           = SDCBaseURL + "sdc-questionnaire-variable"
	SDCRequiredExtension           = SDCBaseURL + "sdc-questionnaire-required"
	SDCSourceStructureMapExtension = SDCBaseURL + "sdc-questionnaire-sourceStructureMap"
	SDCObservationExtractExtension = SDCBaseURL + "sdc-questionnaire-observationExtract"
	SDCObservationLinkPeriodExt    = SDCBaseURL + "sdc-questionnaire-observationLinkPeriod"
	SDCAdditionalDefExtension      = SDCBaseURL + "sdc-questionnaire-additionalDef"
	SDCAssembledFromExtension      = SDCBaseURL + "sdc-questionnaire-assembledFrom"
	SDCCanonicalExtension          = SDCBaseURL + "sdc-questionnaire-canonical"
	SDCSignatureRequiredExtension  = SDCBaseURL + "sdc-questionnaire-signatureRequired"

	QuestionnaireResponseAuthorExtension       = FHIRBaseURL + "questionnaire-responseAuthor"
	QuestionnaireResponseReviewerExtension     = FHIRBaseURL + "questionnaire-responseReviewer"
	QuestionnaireResponseSignatureExtension    = FHIRBaseURL + "questionnaire-responseSignature"
	QuestionnaireCompletionModeExtension       = FHIRBaseURL + "questionnaire-completionMode"
)

// BoundValue is a typed questionnaire min/max value extension value.
type BoundValue struct {
	Value     any    `json:"-"`
	ValueType string `json:"-"`
}

// SupportLink is a questionnaire-supportLink extension value.
type SupportLink struct {
	URL   string `json:"url,omitempty"`
	Label string `json:"label,omitempty"`
}

// LaunchContextDef declares a named launch context on a Questionnaire.
type LaunchContextDef struct {
	Name string   `json:"name,omitempty"`
	Type []string `json:"type,omitempty"`
}

// QuestionnaireVariable is an sdc-questionnaire-variable extension.
type QuestionnaireVariable struct {
	Name       string     `json:"name,omitempty"`
	Expression Expression `json:"expression,omitempty"`
}

// Reference is a lightweight FHIR Reference projection.
type Reference struct {
	Reference string `json:"reference,omitempty"`
	Type      string `json:"type,omitempty"`
	Display   string `json:"display,omitempty"`
}

// Signature is a questionnaire-responseSignature extension value.
type Signature struct {
	Type []Coding `json:"type,omitempty"`
	When string `json:"when,omitempty"`
	Who  Reference `json:"who,omitempty"`
}

// Period is a FHIR Period projection.
type Period struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func finalizeQuestionnaire(q *Questionnaire) {
	if q == nil {
		return
	}
	absorbQuestionnaireExtensions(q)
	walkItems(&q.Item, func(it *Item) { absorbItemBehaviorExtensions(it) })
}

func finalizeQuestionnaireResponse(r *QuestionnaireResponse) {
	if r == nil {
		return
	}
	absorbQuestionnaireResponseExtensions(r)
}

func walkItems(items *[]Item, fn func(*Item)) {
	if items == nil {
		return
	}
	for i := range *items {
		fn(&(*items)[i])
		for j := range (*items)[i].AnswerOption {
			absorbAnswerOptionExtensions(&(*items)[i].AnswerOption[j])
		}
		walkItems(&(*items)[i].Item, fn)
	}
}

func absorbQuestionnaireExtensions(q *Questionnaire) {
	filtered := make([]Extension, 0, len(q.Extension))
	for _, ext := range q.Extension {
		switch ext.URL {
		case SDCLaunchContextExtension:
			if lc, ok := parseLaunchContext(ext); ok {
				q.LaunchContexts = append(q.LaunchContexts, lc)
			}
			continue
		case SDCVariableExtension:
			if v, ok := parseQuestionnaireVariable(ext); ok {
				q.Variables = append(q.Variables, v)
			}
			continue
		case SDCCanonicalExtension:
			if v := extensionScalarString(ext); v != "" {
				q.CanonicalOverride = v
			}
			continue
		case SDCAssembledFromExtension:
			if v := extensionScalarString(ext); v != "" {
				q.AssembledFrom = append(q.AssembledFrom, v)
			}
			continue
		case SDCSourceStructureMapExtension:
			if v := extensionScalarString(ext); v != "" {
				q.SourceStructureMap = v
			}
			continue
		case SDCAdditionalDefExtension:
			if v := extensionScalarString(ext); v != "" {
				q.AdditionalDefinitions = append(q.AdditionalDefinitions, v)
			}
			continue
		case SDCObservationExtractExtension:
			q.ObservationExtract = extensionBoolValue(ext)
			continue
		case SDCObservationLinkPeriodExt:
			if p, ok := extensionPeriod(ext); ok {
				q.ObservationLinkPeriod = &p
			}
			continue
		case SDCSignatureRequiredExtension:
			q.SignatureRequired = extensionBoolValue(ext)
			continue
		}
		filtered = append(filtered, ext)
	}
	q.Extension = filtered
}

func absorbQuestionnaireResponseExtensions(r *QuestionnaireResponse) {
	filtered := make([]Extension, 0, len(r.Extension))
	for _, ext := range r.Extension {
		switch ext.URL {
		case QuestionnaireResponseAuthorExtension:
			if ref, ok := extensionReference(ext); ok {
				r.Author = append(r.Author, ref)
			}
			continue
		case QuestionnaireResponseReviewerExtension:
			if ref, ok := extensionReference(ext); ok {
				r.Reviewer = append(r.Reviewer, ref)
			}
			continue
		case QuestionnaireResponseSignatureExtension:
			if sig, ok := extensionSignature(ext); ok {
				r.Signatures = append(r.Signatures, sig)
			}
			continue
		case QuestionnaireCompletionModeExtension:
			if v := extensionScalarString(ext); v != "" {
				r.CompletionMode = v
			}
			continue
		}
		filtered = append(filtered, ext)
	}
	r.Extension = filtered
}

func absorbItemBehaviorExtensions(it *Item) {
	if it == nil {
		return
	}
	filtered := make([]Extension, 0, len(it.Extension))
	for _, ext := range it.Extension {
		switch ext.URL {
		case QuestionnaireRegexExtension:
			if pattern := extensionScalarString(ext); pattern != "" {
				it.Regex = pattern
			}
			continue
		case QuestionnaireConstraintExtension:
			if constraint, ok := parseItemConstraint(ext); ok {
				it.Constraints = append(it.Constraints, constraint)
			}
			continue
		case MinLengthExtension:
			if v, ok := extensionInt(ext); ok {
				it.MinLength = &v
			}
			continue
		case QuestionnaireMinValueExtension:
			if bv, ok := extensionBoundValue(ext); ok {
				it.MinValue = &bv
			}
			continue
		case QuestionnaireMaxValueExtension:
			if bv, ok := extensionBoundValue(ext); ok {
				it.MaxValue = &bv
			}
			continue
		case QuestionnaireMinOccursExtension:
			if v, ok := extensionInt(ext); ok {
				it.MinOccurs = &v
			}
			continue
		case QuestionnaireMaxOccursExtension:
			if v, ok := extensionInt(ext); ok {
				it.MaxOccurs = &v
			}
			continue
		case QuestionnaireReferenceResourceExt:
			for _, code := range extensionCodeValues(ext) {
				it.ReferenceResources = append(it.ReferenceResources, code)
			}
			continue
		case QuestionnaireReferenceProfileExt:
			if v := extensionScalarString(ext); v != "" {
				it.ReferenceProfiles = append(it.ReferenceProfiles, v)
			}
			continue
		case QuestionnaireReferenceFilterExt:
			if v := extensionScalarString(ext); v != "" {
				it.ReferenceFilter = v
			}
			continue
		case QuestionnaireUnitExtension:
			if c, ok := extensionCoding(ext); ok {
				it.Unit = &c
			}
			continue
		case QuestionnaireUnitOptionExtension:
			if c, ok := extensionCoding(ext); ok {
				it.UnitOptions = append(it.UnitOptions, c)
			}
			continue
		case QuestionnaireUnitValueSetExtension:
			if v := extensionScalarString(ext); v != "" {
				it.UnitValueSet = v
			}
			continue
		case QuestionnaireChoiceOrientationExt:
			if v := extensionCodeScalar(ext); v != "" {
				it.ChoiceOrientation = v
			}
			continue
		case QuestionnaireOptionExclusiveExt:
			it.OptionExclusive = extensionBoolValue(ext)
			continue
		case QuestionnaireSliderStepValueExt:
			if v, ok := extensionDecimal(ext); ok {
				it.SliderStepValue = &v
			}
			continue
		case QuestionnaireUsageModeExtension:
			if v := extensionCodeScalar(ext); v != "" {
				it.UsageMode = v
			}
			continue
		case QuestionnaireDisplayCategoryExt:
			if v := extensionCodeScalar(ext); v != "" {
				it.DisplayCategory = v
			}
			continue
		case QuestionnaireSupportLinkExtension:
			if link, ok := extensionSupportLink(ext); ok {
				it.SupportLinks = append(it.SupportLinks, link)
			}
			continue
		case QuestionnaireFHIRTypeExtension:
			if v := extensionCodeScalar(ext); v != "" {
				it.FHIRType = v
			}
			continue
		case QuestionnaireBaseTypeExtension:
			if v := extensionCodeScalar(ext); v != "" {
				it.BaseType = v
			}
			continue
		}
		filtered = append(filtered, ext)
	}
	it.Extension = filtered
}

func absorbAnswerOptionExtensions(opt *AnswerOption) {
	if opt == nil || len(opt.Extension) == 0 {
		return
	}
	filtered := make([]Extension, 0, len(opt.Extension))
	for _, ext := range opt.Extension {
		if ext.URL == QuestionnaireOptionPrefixExtension {
			if v := extensionScalarString(ext); v != "" {
				opt.OptionPrefix = v
			}
			continue
		}
		filtered = append(filtered, ext)
	}
	opt.Extension = filtered
}

func childURLSuffix(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}

func extensionScalarString(ext Extension) string {
	switch x := ext.Value.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return ""
	}
}

func appendItemBehaviorExtensions(ext []Extension, it Item) []Extension {
	if it.MinLength != nil {
		ext = upsertExtension(ext, Extension{URL: MinLengthExtension, Value: *it.MinLength, valueType: "Integer"})
	}
	if it.MinValue != nil {
		ext = upsertExtension(ext, boundValueExtension(QuestionnaireMinValueExtension, *it.MinValue))
	}
	if it.MaxValue != nil {
		ext = upsertExtension(ext, boundValueExtension(QuestionnaireMaxValueExtension, *it.MaxValue))
	}
	if it.MinOccurs != nil {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireMinOccursExtension, Value: *it.MinOccurs, valueType: "Integer"})
	}
	if it.MaxOccurs != nil {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireMaxOccursExtension, Value: *it.MaxOccurs, valueType: "Integer"})
	}
	for _, rt := range it.ReferenceResources {
		ext = append(ext, Extension{URL: QuestionnaireReferenceResourceExt, Value: rt, valueType: "Code"})
	}
	for _, profile := range it.ReferenceProfiles {
		ext = append(ext, Extension{URL: QuestionnaireReferenceProfileExt, Value: profile, valueType: "Canonical"})
	}
	if it.ReferenceFilter != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireReferenceFilterExt, Value: it.ReferenceFilter, valueType: "String"})
	}
	if it.Unit != nil {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireUnitExtension, Value: *it.Unit, valueType: "Coding"})
	}
	for _, unit := range it.UnitOptions {
		ext = append(ext, Extension{URL: QuestionnaireUnitOptionExtension, Value: unit, valueType: "Coding"})
	}
	if it.UnitValueSet != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireUnitValueSetExtension, Value: it.UnitValueSet, valueType: "Canonical"})
	}
	if it.ChoiceOrientation != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireChoiceOrientationExt, Value: it.ChoiceOrientation, valueType: "Code"})
	}
	if it.OptionExclusive {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireOptionExclusiveExt, Value: true, valueType: "Boolean"})
	}
	if it.SliderStepValue != nil {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireSliderStepValueExt, Value: *it.SliderStepValue, valueType: "Decimal"})
	}
	if it.UsageMode != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireUsageModeExtension, Value: it.UsageMode, valueType: "Code"})
	}
	if it.DisplayCategory != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireDisplayCategoryExt, Value: it.DisplayCategory, valueType: "Code"})
	}
	for _, link := range it.SupportLinks {
		ext = append(ext, Extension{URL: QuestionnaireSupportLinkExtension, Value: link, valueType: "Url"})
	}
	if it.FHIRType != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireFHIRTypeExtension, Value: it.FHIRType, valueType: "Code"})
	}
	if it.BaseType != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireBaseTypeExtension, Value: it.BaseType, valueType: "Code"})
	}
	if it.Regex != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireRegexExtension, Value: it.Regex, valueType: "String"})
	}
	for _, constraint := range it.Constraints {
		ext = append(ext, itemConstraintExtension(constraint))
	}
	return ext
}

func appendQuestionnaireBehaviorExtensions(ext []Extension, q Questionnaire) []Extension {
	for _, lc := range q.LaunchContexts {
		ext = append(ext, launchContextExtension(lc))
	}
	for _, variable := range q.Variables {
		ext = append(ext, questionnaireVariableExtension(variable))
	}
	if q.CanonicalOverride != "" {
		ext = upsertExtension(ext, Extension{URL: SDCCanonicalExtension, Value: q.CanonicalOverride, valueType: "Canonical"})
	}
	for _, from := range q.AssembledFrom {
		ext = append(ext, Extension{URL: SDCAssembledFromExtension, Value: from, valueType: "Canonical"})
	}
	if q.SourceStructureMap != "" {
		ext = upsertExtension(ext, Extension{URL: SDCSourceStructureMapExtension, Value: q.SourceStructureMap, valueType: "Canonical"})
	}
	for _, def := range q.AdditionalDefinitions {
		ext = append(ext, Extension{URL: SDCAdditionalDefExtension, Value: def, valueType: "Canonical"})
	}
	if q.ObservationExtract {
		ext = upsertExtension(ext, Extension{URL: SDCObservationExtractExtension, Value: true, valueType: "Boolean"})
	}
	if q.ObservationLinkPeriod != nil {
		ext = upsertExtension(ext, Extension{URL: SDCObservationLinkPeriodExt, Value: *q.ObservationLinkPeriod, valueType: "Period"})
	}
	if q.SignatureRequired {
		ext = upsertExtension(ext, Extension{URL: SDCSignatureRequiredExtension, Value: true, valueType: "Boolean"})
	}
	return ext
}

func appendResponseBehaviorExtensions(ext []Extension, r QuestionnaireResponse) []Extension {
	for _, author := range r.Author {
		ext = append(ext, Extension{URL: QuestionnaireResponseAuthorExtension, Value: author, valueType: "Reference"})
	}
	for _, reviewer := range r.Reviewer {
		ext = append(ext, Extension{URL: QuestionnaireResponseReviewerExtension, Value: reviewer, valueType: "Reference"})
	}
	for _, sig := range r.Signatures {
		ext = append(ext, Extension{URL: QuestionnaireResponseSignatureExtension, Value: sig})
	}
	if r.CompletionMode != "" {
		ext = upsertExtension(ext, Extension{URL: QuestionnaireCompletionModeExtension, Value: r.CompletionMode, valueType: "Code"})
	}
	return ext
}

func launchContextExtension(lc LaunchContextDef) Extension {
	children := []Extension{{URL: "name", Value: lc.Name, valueType: "Code"}}
	for _, typ := range lc.Type {
		children = append(children, Extension{URL: "type", Value: typ, valueType: "Code"})
	}
	return Extension{URL: SDCLaunchContextExtension, Extension: children}
}

func parseLaunchContext(ext Extension) (LaunchContextDef, bool) {
	if len(ext.Extension) == 0 {
		return LaunchContextDef{}, false
	}
	lc := LaunchContextDef{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "name":
			lc.Name = extensionScalarString(child)
		case "type":
			if v := extensionCodeScalar(child); v != "" {
				lc.Type = append(lc.Type, v)
			}
		}
	}
	return lc, lc.Name != ""
}

func questionnaireVariableExtension(v QuestionnaireVariable) Extension {
	return Extension{
		URL: SDCVariableExtension,
		Extension: []Extension{
			{URL: "name", Value: v.Name, valueType: "Code"},
			{URL: "expression", Value: v.Expression, valueType: "Expression"},
		},
	}
}

func parseQuestionnaireVariable(ext Extension) (QuestionnaireVariable, bool) {
	if ext.URL != SDCVariableExtension {
		return QuestionnaireVariable{}, false
	}
	v := QuestionnaireVariable{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "name":
			v.Name = extensionScalarString(child)
		case "expression":
			if expression, ok := extensionExpression(child); ok {
				v.Expression = expression
			}
		}
	}
	return v, v.Name != "" && v.Expression.Expression != ""
}

func boundValueExtension(url string, bv BoundValue) Extension {
	suffix := firstNonEmpty(bv.ValueType, valueSuffix(bv.Value))
	return Extension{URL: url, Value: bv.Value, valueType: suffix}
}

func extensionBoundValue(ext Extension) (BoundValue, bool) {
	if ext.Value == nil {
		return BoundValue{}, false
	}
	return BoundValue{Value: ext.Value, ValueType: firstNonEmpty(ext.ValueType, ext.valueType, valueSuffix(ext.Value))}, true
}

func extensionInt(ext Extension) (int, bool) {
	switch x := ext.Value.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		if err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func extensionDecimal(ext Extension) (float64, bool) {
	n, ok := number(ext.Value)
	return n, ok
}

func extensionBoolValue(ext Extension) bool {
	if v, ok := ext.Value.(bool); ok {
		return v
	}
	return false
}

func extensionCodeScalar(ext Extension) string {
	if v := extensionScalarString(ext); v != "" {
		return v
	}
	if c, ok := extensionCoding(ext); ok {
		return c.Code
	}
	return ""
}

func extensionCodeValues(ext Extension) []string {
	if v := extensionCodeScalar(ext); v != "" {
		return []string{v}
	}
	return nil
}

func extensionCoding(ext Extension) (Coding, bool) {
	switch x := ext.Value.(type) {
	case Coding:
		return x, x.Code != "" || x.System != ""
	case map[string]any:
		c := Coding{}
		if s, ok := x["system"].(string); ok {
			c.System = s
		}
		if code, ok := x["code"].(string); ok {
			c.Code = code
		}
		if display, ok := x["display"].(string); ok {
			c.Display = display
		}
		return c, c.Code != "" || c.System != ""
	default:
		return Coding{}, false
	}
}

func extensionReference(ext Extension) (Reference, bool) {
	switch x := ext.Value.(type) {
	case Reference:
		return x, x.Reference != "" || x.Type != ""
	case map[string]any:
		ref := Reference{}
		if s, ok := x["reference"].(string); ok {
			ref.Reference = s
		}
		if s, ok := x["type"].(string); ok {
			ref.Type = s
		}
		if s, ok := x["display"].(string); ok {
			ref.Display = s
		}
		return ref, ref.Reference != "" || ref.Type != ""
	default:
		return Reference{}, false
	}
}

func extensionSignature(ext Extension) (Signature, bool) {
	switch x := ext.Value.(type) {
	case Signature:
		return x, true
	case map[string]any:
		sig := Signature{}
		if when, ok := x["when"].(string); ok {
			sig.When = when
		}
		if who, ok := x["who"].(map[string]any); ok {
			if ref, ok := extensionReference(Extension{Value: who}); ok {
				sig.Who = ref
			}
		}
		return sig, sig.When != "" || sig.Who.Reference != ""
	default:
		return Signature{}, false
	}
}

func extensionPeriod(ext Extension) (Period, bool) {
	switch x := ext.Value.(type) {
	case Period:
		return x, true
	case map[string]any:
		p := Period{}
		if s, ok := x["start"].(string); ok {
			p.Start = s
		}
		if s, ok := x["end"].(string); ok {
			p.End = s
		}
		return p, p.Start != "" || p.End != ""
	default:
		return Period{}, false
	}
}

func extensionSupportLink(ext Extension) (SupportLink, bool) {
	switch x := ext.Value.(type) {
	case SupportLink:
		return x, x.URL != ""
	case map[string]any:
		link := SupportLink{}
		if url, ok := x["url"].(string); ok {
			link.URL = url
		}
		if label, ok := x["label"].(string); ok {
			link.Label = label
		}
		return link, link.URL != ""
	case string:
		return SupportLink{URL: x}, x != ""
	default:
		return SupportLink{}, false
	}
}

func itemNavigationHint(it Item) string {
	if it.UsageMode != "" {
		return it.UsageMode
	}
	return it.DisplayCategory
}

func mergeLaunchContext(declared []LaunchContextDef, provided map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range provided {
		merged[k] = v
	}
	for _, lc := range declared {
		if lc.Name == "" || merged[lc.Name] != nil {
			continue
		}
	}
	return merged
}

func variableEvaluationContext(q Questionnaire, pc PopulationContext) map[string]any {
	root := map[string]any{}
	if pc.Subject != nil {
		root["subject"] = pc.Subject
	}
	if len(pc.LaunchContext) > 0 {
		root["launchContext"] = pc.LaunchContext
	}
	vars := map[string]any{}
	for _, variable := range q.Variables {
		if variable.Name != "" {
			vars[variable.Name] = nil
		}
	}
	if len(vars) > 0 {
		root["variable"] = vars
	}
	return root
}

func evaluateQuestionnaireVariables(ctx context.Context, q Questionnaire, pc PopulationContext, provider ExpressionProvider) map[string]any {
	vars := map[string]any{}
	if provider == nil {
		return vars
	}
	input := variableEvaluationContext(q, pc)
	for _, variable := range q.Variables {
		if variable.Name == "" || variable.Expression.Expression == "" {
			continue
		}
		values, err := provider.Evaluate(ctx, variable.Expression, input)
		if err != nil || len(values) == 0 {
			continue
		}
		vars[variable.Name] = values[0]
	}
	return vars
}

func formatBoundValue(v BoundValue) string {
	if v.Value == nil {
		return ""
	}
	switch x := v.Value.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}
