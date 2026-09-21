package cql

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// StoreRetriever retrieves FHIR resources from a ResourceStore, filtered by
// the Patient subject when present. When References is set, retrieve uses
// subject/patient reference lookups instead of listing every resource of the type.
type StoreRetriever struct {
	Resources  store.ResourceStore
	References ReferenceLookup
}

// ReferenceLookup finds resources that reference a given target (optional search index).
type ReferenceLookup interface {
	LookupReferencing(ctx context.Context, sourceType, fieldKey, targetType, targetID string) ([]string, error)
}

func (r StoreRetriever) Retrieve(ctx context.Context, req RetrieveRequest, patient any) ([]any, error) {
	if r.Resources == nil {
		return nil, errf("CQL retrieve store is unavailable")
	}
	if req.ResourceType == "" {
		return nil, errf("CQL retrieve is missing a resource type")
	}
	wantRef := patientReference(patient)
	ids, err := r.candidateIDs(ctx, req, wantRef)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, id := range ids {
		env, err := r.Resources.Read(ctx, req.ResourceType, id)
		if err != nil || env == nil {
			continue
		}
		if wantRef != "" && !resourceMatchesPatient(env, wantRef) {
			continue
		}
		out = append(out, env)
	}
	return out, nil
}

func (r StoreRetriever) candidateIDs(ctx context.Context, req RetrieveRequest, wantRef string) ([]string, error) {
	if strings.EqualFold(req.ResourceType, "Patient") && wantRef != "" {
		if id := referenceID(wantRef); id != "" {
			return []string{id}, nil
		}
	}
	if r.References != nil && wantRef != "" {
		targetType, targetID := splitReference(wantRef)
		if targetID != "" {
			if targetType == "" {
				targetType = "Patient"
			}
			var ids []string
			lookedUp := false
			for _, field := range []string{"reference.subject", "reference.patient"} {
				found, err := r.References.LookupReferencing(ctx, req.ResourceType, field, targetType, targetID)
				if err != nil {
					continue
				}
				lookedUp = true
				ids = append(ids, found...)
			}
			if lookedUp {
				return uniqueIDs(ids), nil
			}
		}
	}
	return r.Resources.ListIDs(ctx, req.ResourceType, 10000, 0)
}

// StaticRetriever returns a fixed collection, optionally filtered by resource type.
type StaticRetriever []any

func (r StaticRetriever) Retrieve(_ context.Context, req RetrieveRequest, _ any) ([]any, error) {
	if req.ResourceType == "" {
		return append([]any(nil), r...), nil
	}
	var out []any
	for _, item := range r {
		if resourceTypeOf(item) == req.ResourceType {
			out = append(out, item)
		}
	}
	return out, nil
}

func patientReference(patient any) string {
	if patient == nil {
		return ""
	}
	if env, ok := patient.(*types.ResourceEnvelope); ok && env != nil {
		if env.ResourceType != "" && env.ID != "" {
			return env.ResourceType + "/" + env.ID
		}
	}
	obj, ok := asObject(patient)
	if !ok {
		return ""
	}
	rt, _ := obj["resourceType"].(string)
	id, _ := obj["id"].(string)
	if rt != "" && id != "" {
		return rt + "/" + id
	}
	if id != "" {
		return "Patient/" + id
	}
	return ""
}

func resourceMatchesPatient(env *types.ResourceEnvelope, wantRef string) bool {
	if env == nil {
		return false
	}
	if env.ResourceType == "Patient" {
		return referenceMatches(env.ResourceType+"/"+env.ID, wantRef)
	}
	obj, ok := asObject(env)
	if !ok {
		return false
	}
	subj, _ := obj["subject"].(map[string]any)
	if subj == nil {
		subj, _ = obj["patient"].(map[string]any)
	}
	if subj == nil {
		return false
	}
	ref, _ := subj["reference"].(string)
	return referenceMatches(ref, wantRef)
}

func referenceMatches(ref, wantRef string) bool {
	ref = strings.TrimSpace(ref)
	wantRef = strings.TrimSpace(wantRef)
	if ref == "" || wantRef == "" {
		return false
	}
	if strings.EqualFold(ref, wantRef) {
		return true
	}
	gotType, gotID := splitReference(ref)
	wantType, wantID := splitReference(wantRef)
	if gotID == "" || wantID == "" || !strings.EqualFold(gotID, wantID) {
		return false
	}
	if gotType != "" && wantType != "" && !strings.EqualFold(gotType, wantType) {
		return false
	}
	return true
}

func splitReference(ref string) (resourceType, id string) {
	ref = strings.TrimSpace(ref)
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimSuffix(ref, "/")
	if ref == "" {
		return "", ""
	}
	i := strings.LastIndex(ref, "/")
	if i < 0 {
		return "", ref
	}
	id = ref[i+1:]
	rest := ref[:i]
	j := strings.LastIndex(rest, "/")
	if j >= 0 {
		return rest[j+1:], id
	}
	return rest, id
}

func referenceID(ref string) string {
	_, id := splitReference(ref)
	return id
}

func uniqueIDs(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func matchResourceTerminology(ctx context.Context, item any, req RetrieveRequest, term fhirpath.TerminologyValidator, resolve func(string) []fhirCoding) (bool, error) {
	if req.Terminology == "" && req.ValueSetURL == "" && req.Code == "" {
		return true, nil
	}
	codes := extractCodings(item, req.CodePath, resolve)
	if len(codes) == 0 && req.CodePath == "" {
		if c := codingFromValue(item); c.Code != "" || c.Display != "" || c.Text != "" || c.System != "" {
			codes = []fhirCoding{c}
		}
	}
	switch req.Comparator {
	case "=":
		return codingMatchesExact(codes, req.System, req.Code, req.Terminology), nil
	case "~":
		return codingMatchesEquivalent(codes, req.System, req.Code, req.Terminology), nil
	}
	if req.Code != "" {
		return codingMatches(codes, req.System, req.Code, ""), nil
	}
	if req.ValueSetURL != "" {
		if term == nil {
			return false, errf("%w: valueset %s requires a terminology service", ErrUnsupported, req.ValueSetURL)
		}
		for _, c := range codes {
			if c.Code == "" {
				continue
			}
			ok, err := term.MemberOf(ctx, req.ValueSetURL, c.System, c.Code)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	return codingMatches(codes, "", "", req.Terminology), nil
}

type fhirCoding struct {
	System  string
	Code    string
	Display string
	Text    string
}

func extractCodings(v any, codePath string, resolve func(string) []fhirCoding) []fhirCoding {
	obj, ok := asObject(v)
	if !ok {
		return nil
	}
	var out []fhirCoding
	rt, _ := obj["resourceType"].(string)
	if rt == "" && codePath == "" {
		collectCodeable(obj, &out)
		return out
	}
	fields := primaryCodeFields
	if codePath != "" {
		fields = matchResourceFields(obj, codePath)
		if len(fields) == 0 {
			return nil
		}
	}
	for _, field := range fields {
		raw, exists := obj[field]
		if !exists {
			continue
		}
		n := len(out)
		collectCodeable(raw, &out)
		if ref := referenceString(raw); ref != "" && resolve != nil {
			out = append(out, resolve(ref)...)
		}
		if len(out) == n {
			if c := codingFromValue(raw); c.Code != "" || c.Display != "" || c.Text != "" || c.System != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

func referenceString(v any) string {
	obj, ok := asObject(v)
	if !ok {
		return ""
	}
	ref, _ := obj["reference"].(string)
	return strings.TrimSpace(ref)
}

func matchResourceFields(obj map[string]any, name string) []string {
	var fold, choice []string
	for k := range obj {
		if k == name {
			return []string{k}
		}
		if strings.EqualFold(k, name) {
			fold = append(fold, k)
			continue
		}
		if isCodeableChoiceField(k, name) {
			choice = append(choice, k)
		}
	}
	if len(fold) > 0 {
		return fold
	}
	return choice
}

func isCodeableChoiceField(key, name string) bool {
	if len(key) <= len(name) {
		return false
	}
	if !strings.EqualFold(key[:len(name)], name) {
		return false
	}
	rest := key[len(name):]
	if rest == "" || rest[0] < 'A' || rest[0] > 'Z' {
		return false
	}
	if strings.EqualFold(rest, "CodeableConcept") || strings.EqualFold(rest, "Coding") {
		return true
	}
	return strings.EqualFold(rest, "Reference") && strings.EqualFold(name, "medication")
}

// primaryCodeFields are the implicit FHIR elements CQL retrieve filters on
// when no property path is named. Nested Quantity, Annotation, component,
// category, class, reasonCode, bodySite, value, and meta.tag are ignored.
var primaryCodeFields = []string{
	"code",
	"type",
	"medicationCodeableConcept",
	"medication",
	"medicationReference",
	"vaccineCode",
}

func collectCodeable(v any, out *[]fhirCoding) {
	switch x := v.(type) {
	case []any:
		for _, el := range x {
			collectCodeable(el, out)
		}
	case map[string]any:
		if raw, ok := x["coding"].([]any); ok {
			text := strField(x, "text")
			for _, el := range raw {
				m, ok := el.(map[string]any)
				if !ok {
					continue
				}
				appendCoding(m, text, out)
			}
			if text != "" && len(raw) == 0 {
				*out = append(*out, fhirCoding{Text: text})
			}
			return
		}
		if isCoding(x) {
			appendCoding(x, "", out)
			return
		}
		if isTextOnlyCodeableConcept(x) {
			*out = append(*out, fhirCoding{Text: strField(x, "text")})
		}
	}
}

func appendCoding(m map[string]any, text string, out *[]fhirCoding) {
	if isQuantity(m) {
		return
	}
	*out = append(*out, fhirCoding{
		System:  strField(m, "system"),
		Code:    strField(m, "code"),
		Display: strField(m, "display"),
		Text:    text,
	})
}

func isQuantity(m map[string]any) bool {
	if _, ok := m["unit"]; ok {
		return true
	}
	switch m["value"].(type) {
	case float64, float32, int, int32, int64, json.Number:
		return true
	}
	return false
}

func isCoding(m map[string]any) bool {
	if isQuantity(m) {
		return false
	}
	code, _ := m["code"].(string)
	if code == "" {
		return false
	}
	_, hasSystem := m["system"]
	_, hasDisplay := m["display"]
	return hasSystem || hasDisplay
}

func isTextOnlyCodeableConcept(m map[string]any) bool {
	if strField(m, "text") == "" {
		return false
	}
	for k := range m {
		switch k {
		case "id", "extension", "text", "coding":
		default:
			return false
		}
	}
	return true
}

func codingFromValue(v any) fhirCoding {
	switch x := v.(type) {
	case Code:
		return fhirCoding{System: x.System, Code: x.Code, Display: x.Display}
	case fhirCoding:
		return x
	case string:
		if sys, code, ok := splitSystemCode(x); ok {
			return fhirCoding{System: sys, Code: code}
		}
		return fhirCoding{Code: x, Text: x, Display: x}
	}
	if obj, ok := asObject(v); ok {
		if isCoding(obj) {
			return fhirCoding{System: strField(obj, "system"), Code: strField(obj, "code"), Display: strField(obj, "display")}
		}
		if raw, ok := obj["coding"].([]any); ok {
			text := strField(obj, "text")
			if len(raw) > 0 {
				if m, ok := raw[0].(map[string]any); ok {
					return fhirCoding{
						System:  strField(m, "system"),
						Code:    strField(m, "code"),
						Display: strField(m, "display"),
						Text:    text,
					}
				}
			}
			return fhirCoding{Text: text}
		}
		return fhirCoding{Text: strField(obj, "text"), Code: strField(obj, "code")}
	}
	return fhirCoding{}
}

func codingMatchesExact(codes []fhirCoding, system, code, term string) bool {
	if code == "" && strings.Contains(term, ";") {
		for _, part := range strings.Split(term, ";") {
			part = strings.TrimSpace(part)
			if part != "" && codingMatchesExact(codes, "", "", part) {
				return true
			}
		}
		return false
	}
	for _, c := range codes {
		if code != "" {
			if c.Code != code {
				continue
			}
			if system != "" && c.System != system {
				continue
			}
			return true
		}
		if term == "" {
			continue
		}
		if c.Code == term {
			return true
		}
		if sys, cd, ok := splitSystemCode(term); ok {
			if (sys == "" || c.System == sys) && c.Code == cd {
				return true
			}
		}
	}
	return false
}

func codingMatchesEquivalent(codes []fhirCoding, system, code, term string) bool {
	if code == "" && strings.Contains(term, ";") {
		for _, part := range strings.Split(term, ";") {
			part = strings.TrimSpace(part)
			if part != "" && codingMatchesEquivalent(codes, "", "", part) {
				return true
			}
		}
		return false
	}
	want := code
	if want == "" {
		want = term
	}
	if want == "" {
		return false
	}
	for _, c := range codes {
		matched := strings.EqualFold(c.Code, want) || strings.EqualFold(c.Display, want) || strings.EqualFold(c.Text, want)
		if matched {
			if system != "" && strings.EqualFold(c.Code, want) && !strings.EqualFold(c.System, system) {
				continue
			}
			return true
		}
		if sys, cd, ok := splitSystemCode(term); ok {
			if (sys == "" || strings.EqualFold(c.System, sys)) && (strings.EqualFold(c.Code, cd) || strings.EqualFold(c.Display, cd) || strings.EqualFold(c.Text, cd)) {
				return true
			}
		}
	}
	return false
}

func codingMatches(codes []fhirCoding, system, code, term string) bool {
	if code == "" && strings.Contains(term, ";") {
		for _, part := range strings.Split(term, ";") {
			part = strings.TrimSpace(part)
			if part != "" && codingMatches(codes, "", "", part) {
				return true
			}
		}
		return false
	}
	for _, c := range codes {
		if code != "" {
			if c.Code != code {
				continue
			}
			if system != "" && c.System != system {
				continue
			}
			return true
		}
		if term == "" {
			continue
		}
		if c.Code == term {
			return true
		}
		if sys, cd, ok := splitSystemCode(term); ok {
			if (sys == "" || c.System == sys) && c.Code == cd {
				return true
			}
		}
	}
	return false
}

func splitSystemCode(term string) (system, code string, ok bool) {
	term = strings.TrimSpace(term)
	i := strings.LastIndex(term, "|")
	if i <= 0 || i == len(term)-1 {
		return "", "", false
	}
	return strings.TrimSpace(term[:i]), strings.TrimSpace(term[i+1:]), true
}

func strField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return ""
}

func resourceTypeOf(v any) string {
	if env, ok := v.(*types.ResourceEnvelope); ok && env != nil {
		return env.ResourceType
	}
	obj, ok := asObject(v)
	if !ok {
		return ""
	}
	rt, _ := obj["resourceType"].(string)
	return rt
}
