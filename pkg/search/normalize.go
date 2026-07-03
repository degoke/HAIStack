package search

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
	"google.golang.org/protobuf/proto"
)

// fieldKeyForParam maps a search parameter code and FHIR type to a typed index field key.
func fieldKeyForParam(code, paramType string) string {
	switch code {
	case "_id":
		return "token._id"
	case "_lastUpdated":
		return "date._lastUpdated"
	case "phone":
		return "string.phone"
	}
	switch paramType {
	case "token":
		return "token." + code
	case "string":
		return "string." + code
	case "date":
		return "date." + code
	case "reference":
		return "reference." + code
	case "number":
		return "number." + code
	default:
		return ""
	}
}

// normalizeValues converts FHIRPath results into stable index strings for one parameter.
func normalizeValues(code, paramType string, values []fhirpath.Value) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, v := range values {
		for _, s := range normalizeValue(code, paramType, v) {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func normalizeValue(code, paramType string, v fhirpath.Value) []string {
	if v.Raw() == nil {
		return nil
	}
	switch code {
	case "phone":
		return normalizeStringValue(v)
	}
	switch paramType {
	case "string":
		return normalizeStringValue(v)
	case "token":
		return normalizeTokenValue(v)
	case "date":
		return normalizeDateValue(v)
	case "reference":
		return normalizeReferenceValue(v)
	case "number":
		return normalizeNumberValue(v)
	default:
		return nil
	}
}

func normalizeStringValue(v fhirpath.Value) []string {
	switch val := v.Raw().(type) {
	case *dtpb.HumanName:
		var out []string
		if text := val.GetText(); text != nil && text.GetValue() != "" {
			out = append(out, text.GetValue())
		}
		if family := val.GetFamily().GetValue(); family != "" {
			out = append(out, family)
		}
		for _, g := range val.GetGiven() {
			if given := g.GetValue(); given != "" {
				out = append(out, given)
			}
		}
		var parts []string
		parts = append(parts, val.GetFamily().GetValue())
		for _, g := range val.GetGiven() {
			parts = append(parts, g.GetValue())
		}
		if joined := strings.TrimSpace(strings.Join(parts, " ")); joined != "" {
			out = append(out, joined)
		}
		return out
	case *dtpb.ContactPoint:
		if val.GetValue() != nil {
			if s := val.GetValue().GetValue(); s != "" {
				return []string{s}
			}
		}
	case proto.Message:
		if s, err := v.String(); err == nil && s != "" {
			return []string{s}
		}
	default:
		if s, err := v.String(); err == nil && s != "" {
			return []string{s}
		}
	}
	return nil
}

func normalizeTokenValue(v fhirpath.Value) []string {
	if tokens := enumTokenValue(v.Raw()); len(tokens) > 0 {
		return tokens
	}
	switch val := v.Raw().(type) {
	case *dtpb.Identifier:
		return identifierTokens(val)
	case *dtpb.CodeableConcept:
		return codeableConceptTokens(val)
	case *dtpb.Coding:
		return codingTokens(val)
	case *dtpb.Code:
		if s := val.GetValue(); s != "" {
			return []string{s}
		}
	case *dtpb.ContactPoint:
		if val.GetValue() != nil {
			if s := val.GetValue().GetValue(); s != "" {
				return []string{s}
			}
		}
	default:
		if s, err := v.String(); err == nil && s != "" {
			return []string{s}
		}
	}
	return nil
}

func identifierTokens(id *dtpb.Identifier) []string {
	if id == nil {
		return nil
	}
	system := id.GetSystem().GetValue()
	value := id.GetValue().GetValue()
	if value == "" {
		return nil
	}
	if system != "" {
		return []string{system + "|" + value, value}
	}
	return []string{value}
}

func codeableConceptTokens(cc *dtpb.CodeableConcept) []string {
	if cc == nil {
		return nil
	}
	var out []string
	for _, coding := range cc.GetCoding() {
		out = append(out, codingTokens(coding)...)
	}
	if text := cc.GetText(); text != nil && text.GetValue() != "" {
		out = append(out, text.GetValue())
	}
	return out
}

func codingTokens(c *dtpb.Coding) []string {
	if c == nil {
		return nil
	}
	system := c.GetSystem().GetValue()
	code := c.GetCode().GetValue()
	if code == "" {
		return nil
	}
	if system != "" {
		return []string{system + "|" + code, code}
	}
	return []string{code}
}

func normalizeDateValue(v fhirpath.Value) []string {
	if dates := extractDatesFromChoice(v.Raw()); len(dates) > 0 {
		return dates
	}
	var base []string
	switch val := v.Raw().(type) {
	case system.Date:
		base = []string{val.String()}
	case system.DateTime:
		base = []string{formatDateTime(val.String())}
	case *dtpb.Date:
		d, err := system.DateFromProto(val)
		if err != nil {
			return nil
		}
		base = []string{d.String()}
	case *dtpb.DateTime:
		d, err := system.DateTimeFromProto(val)
		if err != nil {
			return nil
		}
		base = []string{formatDateTime(d.String())}
	default:
		if s, err := v.String(); err == nil && s != "" {
			base = []string{formatDateTime(s)}
		}
	}
	return expandDateIndexValues(base)
}

func expandDateIndexValues(values []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, value := range values {
		for _, v := range dateIndexVariants(value) {
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func dateIndexVariants(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if i := strings.Index(value, "T"); i > 0 {
		return []string{value, value[:i]}
	}
	return []string{value}
}

func normalizeReferenceValue(v fhirpath.Value) []string {
	switch val := v.Raw().(type) {
	case *dtpb.Reference:
		return referenceTokens(val)
	default:
		if s, err := v.String(); err == nil && s != "" {
			return []string{normalizeReferenceString(s)}
		}
	}
	return nil
}

func referenceTokens(ref *dtpb.Reference) []string {
	if ref == nil {
		return nil
	}
	if uri := ref.GetUri(); uri != nil && uri.GetValue() != "" {
		return []string{normalizeReferenceString(uri.GetValue())}
	}
	type id struct {
		getType func() string
		getID   func() string
	}
	var candidate id
	switch {
	case ref.GetPatientId() != nil:
		candidate = id{func() string { return "Patient" }, ref.GetPatientId().GetValue}
	case ref.GetEncounterId() != nil:
		candidate = id{func() string { return "Encounter" }, ref.GetEncounterId().GetValue}
	case ref.GetPractitionerId() != nil:
		candidate = id{func() string { return "Practitioner" }, ref.GetPractitionerId().GetValue}
	case ref.GetRelatedPersonId() != nil:
		candidate = id{func() string { return "RelatedPerson" }, ref.GetRelatedPersonId().GetValue}
	case ref.GetOrganizationId() != nil:
		candidate = id{func() string { return "Organization" }, ref.GetOrganizationId().GetValue}
	case ref.GetDeviceId() != nil:
		candidate = id{func() string { return "Device" }, ref.GetDeviceId().GetValue}
	case ref.GetGroupId() != nil:
		candidate = id{func() string { return "Group" }, ref.GetGroupId().GetValue}
	case ref.GetHealthcareServiceId() != nil:
		candidate = id{func() string { return "HealthcareService" }, ref.GetHealthcareServiceId().GetValue}
	case ref.GetLocationId() != nil:
		candidate = id{func() string { return "Location" }, ref.GetLocationId().GetValue}
	case ref.GetMedicationId() != nil:
		candidate = id{func() string { return "Medication" }, ref.GetMedicationId().GetValue}
	case ref.GetResourceId() != nil:
		candidate = id{func() string { return "" }, ref.GetResourceId().GetValue}
	}
	if candidate.getID == nil {
		return nil
	}
	idVal := candidate.getID()
	if idVal == "" {
		return nil
	}
	if rt := candidate.getType(); rt != "" {
		typed := rt + "/" + idVal
		return []string{typed, idVal}
	}
	return []string{idVal}
}

func normalizeReferenceString(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "urn:") {
		return raw
	}
	if i := strings.LastIndex(raw, "/"); i >= 0 && i < len(raw)-1 {
		return raw[i+1:]
	}
	return raw
}

func normalizeNumberValue(v fhirpath.Value) []string {
	if f, err := v.Float64(); err == nil {
		return []string{fmt.Sprintf("%g", f)}
	}
	if s, err := v.String(); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

func formatDateTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return raw
}

func envelopeLastUpdatedValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func enumTokenValue(raw any) []string {
	if raw == nil {
		return nil
	}
	rv := reflect.ValueOf(raw)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil
	}
	method := rv.MethodByName("GetValue")
	if !method.IsValid() || method.Type().NumIn() != 0 {
		return nil
	}
	out := method.Call(nil)
	if len(out) == 0 {
		return nil
	}
	enumVal := out[0]
	switch enumVal.Kind() {
	case reflect.Int32, reflect.Int, reflect.Uint32:
	default:
		return nil
	}
	if enumVal.Int() == 0 {
		return nil
	}
	if s, ok := enumVal.Interface().(fmt.Stringer); ok {
		token := strings.ToLower(s.String())
		if token != "" {
			return []string{token}
		}
	}
	return nil
}

func extractDatesFromChoice(raw any) []string {
	if raw == nil {
		return nil
	}
	rv := reflect.ValueOf(raw)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil
	}
	if method := rv.MethodByName("GetDateTime"); method.IsValid() && method.Type().NumIn() == 0 {
		out := method.Call(nil)
		if len(out) > 0 && !out[0].IsNil() {
			return normalizeDateValue(fhirpath.NewValue(out[0].Interface()))
		}
	}
	if method := rv.MethodByName("GetPeriod"); method.IsValid() && method.Type().NumIn() == 0 {
		out := method.Call(nil)
		if len(out) > 0 && !out[0].IsNil() {
			if period, ok := out[0].Interface().(*dtpb.Period); ok {
				if start := period.GetStart(); start != nil {
					return normalizeDateValue(fhirpath.NewValue(start))
				}
			}
		}
	}
	return nil
}
