package cql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// MeasureRequest is the input to CQF Measure evaluation.
type MeasureRequest struct {
	// Measure is a FHIR Measure resource.
	Measure *types.ResourceEnvelope
	// PeriodStart and PeriodEnd bound the Measurement Period parameter.
	PeriodStart time.Time
	PeriodEnd   time.Time
	// ReportType is individual, subject-list, or summary (population).
	ReportType string
	// Subject is Patient/{id} when evaluating an individual report.
	Subject string
	// Patient is the already-loaded subject for individual reports.
	Patient any
	// Patients is the population for summary and subject-list reports.
	Patients []any
	// Libraries are compiled CQL libraries referenced by the Measure.
	Libraries []*Library
	// Parameters overlay CQL parameter values (Measurement Period is filled from the period).
	Parameters map[string]any
	// Retriever is used for clinical retrieve during evaluation.
	Retriever Retriever
}

// EvaluateMeasure runs a FHIR Measure against Patient context(s) and returns a MeasureReport.
func (e *Engine) EvaluateMeasure(ctx context.Context, req MeasureRequest) (*types.ResourceEnvelope, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Measure == nil || len(req.Measure.JSON) == 0 {
		return nil, errf("%w: Measure resource is required", ErrMeasure)
	}
	m, err := parseMeasure(req.Measure)
	if err != nil {
		return nil, err
	}
	reportType := normalizeReportType(req.ReportType, req.Subject, req.Patient)
	periodStart, periodEnd := req.PeriodStart, closedPeriodEnd(req.PeriodEnd)
	if periodStart.IsZero() || periodEnd.IsZero() {
		return nil, errf("%w: periodStart and periodEnd are required", ErrMeasure)
	}
	if !periodEnd.After(periodStart) && !periodEnd.Equal(periodStart) {
		return nil, errf("%w: periodEnd must be on or after periodStart", ErrMeasure)
	}
	params := map[string]any{}
	for k, v := range req.Parameters {
		params[k] = v
	}
	params["Measurement Period"] = Interval{
		Low:        periodStart,
		High:       periodEnd,
		LowClosed:  true,
		HighClosed: true,
	}
	subjects, err := measureSubjects(req, reportType)
	if err != nil {
		return nil, err
	}
	env := EvalContext{
		Parameters: params,
		Libraries:  req.Libraries,
		Now:        periodEnd,
		Retriever:  req.Retriever,
	}
	return buildMeasureReport(ctx, e, m, reportType, periodStart, periodEnd, subjects, env)
}

func measureSubjects(req MeasureRequest, reportType string) ([]any, error) {
	if reportType == "individual" {
		if req.Patient != nil {
			return []any{req.Patient}, nil
		}
		if len(req.Patients) == 1 {
			return req.Patients, nil
		}
		if len(req.Patients) > 1 {
			return nil, errf("%w: individual report requires a single Patient", ErrMeasure)
		}
		return nil, errf("%w: individual report requires a subject Patient", ErrMeasure)
	}
	if len(req.Patients) == 0 {
		if req.Patient != nil {
			return []any{req.Patient}, nil
		}
		return nil, errf("%w: summary report requires a patient population", ErrMeasure)
	}
	return req.Patients, nil
}

func closedPeriodEnd(end time.Time) time.Time {
	if end.IsZero() {
		return end
	}
	utc := end.UTC()
	if utc.Hour() == 0 && utc.Minute() == 0 && utc.Second() == 0 && utc.Nanosecond() == 0 {
		return time.Date(utc.Year(), utc.Month(), utc.Day(), 23, 59, 59, 999999999, time.UTC)
	}
	return utc
}

func normalizeReportType(reportType, subject string, patient any) string {
	switch strings.ToLower(strings.TrimSpace(reportType)) {
	case "individual", "subject":
		return "individual"
	case "subject-list", "subjectlist":
		return "subject-list"
	case "summary", "population":
		return "summary"
	}
	if patient != nil || strings.TrimSpace(subject) != "" {
		return "individual"
	}
	return "summary"
}

type fhirMeasure struct {
	ResourceType        string   `json:"resourceType"`
	ID                  string   `json:"id"`
	URL                 string   `json:"url"`
	Name                string   `json:"name"`
	Title               string   `json:"title"`
	Version             string   `json:"version"`
	Library             []string `json:"library"`
	Scoring             fhirConcept
	ImprovementNotation fhirConcept
	SupplementalData    []fhirMeasurePopulation `json:"supplementalData"`
	Group               []fhirMeasureGroup
}

type fhirConcept struct {
	Coding []struct {
		System string `json:"system"`
		Code   string `json:"code"`
	} `json:"coding"`
	Text string `json:"text"`
}

func (c fhirConcept) code() string {
	for _, coding := range c.Coding {
		if coding.Code != "" {
			return coding.Code
		}
	}
	return c.Text
}

type fhirMeasureGroup struct {
	ID         string `json:"id"`
	Code       fhirConcept
	Population []fhirMeasurePopulation
	Stratifier []fhirMeasureStratifier
}

type fhirMeasurePopulation struct {
	ID       string `json:"id"`
	Code     fhirConcept
	Criteria struct {
		Language   string `json:"language"`
		Expression string `json:"expression"`
	} `json:"criteria"`
}

type fhirMeasureStratifier struct {
	ID       string `json:"id"`
	Code     fhirConcept
	Criteria struct {
		Language   string `json:"language"`
		Expression string `json:"expression"`
	} `json:"criteria"`
}

func parseMeasure(env *types.ResourceEnvelope) (fhirMeasure, error) {
	var m fhirMeasure
	if err := json.Unmarshal(env.JSON, &m); err != nil {
		return fhirMeasure{}, errf("%w: decode Measure: %v", ErrMeasure, err)
	}
	if m.ResourceType != "" && m.ResourceType != "Measure" {
		return fhirMeasure{}, errf("%w: expected Measure resource, got %s", ErrMeasure, m.ResourceType)
	}
	if len(m.Group) == 0 {
		return fhirMeasure{}, errf("%w: Measure has no group populations", ErrMeasure)
	}
	return m, nil
}

func buildMeasureReport(ctx context.Context, e *Engine, m fhirMeasure, reportType string, start, end time.Time, subjects []any, env EvalContext) (*types.ResourceEnvelope, error) {
	scoring := strings.ToLower(m.Scoring.code())
	report := map[string]any{
		"resourceType": "MeasureReport",
		"status":       "complete",
		"type":         reportType,
		"date":         e.clock().UTC().Format(time.RFC3339),
		"period": map[string]any{
			"start": start.UTC().Format("2006-01-02"),
			"end":   end.UTC().Format("2006-01-02"),
		},
	}
	if m.URL != "" {
		report["measure"] = m.URL
		if m.Version != "" {
			report["measure"] = m.URL + "|" + m.Version
		}
	}
	if code := m.ImprovementNotation.code(); code != "" {
		report["improvementNotation"] = conceptJSON(m.ImprovementNotation)
	}
	if reportType == "individual" && len(subjects) == 1 {
		if ref := patientReference(subjects[0]); ref != "" {
			report["subject"] = map[string]any{"reference": ref}
			report["evaluatedResource"] = []any{map[string]any{"reference": ref}}
		}
	}
	var groups []any
	var contained []any
	for gi, g := range m.Group {
		groupOut, lists, err := evalMeasureGroup(ctx, e, g, gi, scoring, reportType, subjects, env)
		if err != nil {
			return nil, err
		}
		groups = append(groups, groupOut)
		contained = append(contained, lists...)
	}
	sdeContained, sdeRefs, err := evalSupplementalData(ctx, e, m, subjects, env)
	if err != nil {
		return nil, err
	}
	contained = append(contained, sdeContained...)
	if len(sdeRefs) > 0 {
		evalRes, _ := report["evaluatedResource"].([]any)
		report["evaluatedResource"] = append(evalRes, sdeRefs...)
	}
	report["group"] = groups
	if len(contained) > 0 {
		report["contained"] = contained
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, errf("%w: encode MeasureReport: %v", ErrMeasure, err)
	}
	return types.NewJSONCodec().ParseJSON("MeasureReport", raw)
}

type popEval struct {
	def      fhirMeasurePopulation
	code     string
	count    int
	subjects []string
}

func evalMeasureGroup(ctx context.Context, e *Engine, g fhirMeasureGroup, index int, scoring, reportType string, subjects []any, env EvalContext) (map[string]any, []any, error) {
	pops := make([]popEval, len(g.Population))
	for i, p := range g.Population {
		pops[i] = popEval{def: p, code: strings.ToLower(p.Code.code())}
	}
	type stratumKey struct {
		strat int
		value string
	}
	hasIP := false
	for _, p := range pops {
		if p.code == "initial-population" {
			hasIP = true
			break
		}
	}
	stratumPops := map[stratumKey]map[string]int{}
	for _, subject := range subjects {
		env.Patient = subject
		inIP := !hasIP
		matched := map[string]bool{}
		for i := range pops {
			ok, _, err := evalPopulation(ctx, e, pops[i].def, env)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				continue
			}
			pops[i].count++
			matched[pops[i].code] = true
			if pops[i].code == "initial-population" {
				inIP = true
			}
			if reportType == "subject-list" {
				if ref := patientReference(subject); ref != "" {
					pops[i].subjects = append(pops[i].subjects, ref)
				}
			}
		}
		if !inIP {
			continue
		}
		for si, strat := range g.Stratifier {
			expr := strings.TrimSpace(strat.Criteria.Expression)
			if expr == "" {
				continue
			}
			env.Language = strat.Criteria.Language
			if env.Language == "" {
				env.Language = "text/cql.identifier"
			}
			vals, err := e.Eval(ctx, expr, env)
			if err != nil {
				return nil, nil, errf("%w: stratifier: %v", ErrMeasure, err)
			}
			key := stratumKey{strat: si, value: fmt.Sprint(singletonOrList(vals))}
			counts := stratumPops[key]
			if counts == nil {
				counts = map[string]int{}
				stratumPops[key] = counts
			}
			for _, p := range pops {
				if matched[p.code] {
					counts[p.code]++
				}
			}
		}
	}
	rawCounts := populationCountMap(pops)
	scoreCounts := applyPopulationExclusions(rawCounts, scoring)
	out := map[string]any{}
	if g.ID != "" {
		out["id"] = g.ID
	} else {
		out["id"] = "group-" + itoa(index)
	}
	if code := g.Code.code(); code != "" {
		out["code"] = conceptJSON(g.Code)
	}
	var popOut []any
	var contained []any
	for pi, p := range pops {
		item := map[string]any{
			"code":  conceptJSON(p.def.Code),
			"count": p.count,
		}
		if p.def.ID != "" {
			item["id"] = p.def.ID
		}
		if reportType == "subject-list" && len(p.subjects) > 0 {
			listID := "list-" + itoa(index) + "-" + itoa(pi)
			list := map[string]any{
				"resourceType": "List",
				"id":           listID,
				"status":       "current",
				"mode":         "snapshot",
			}
			var entries []any
			for _, ref := range p.subjects {
				entries = append(entries, map[string]any{"item": map[string]any{"reference": ref}})
			}
			list["entry"] = entries
			contained = append(contained, list)
			item["subjectResults"] = map[string]any{"reference": "#" + listID}
		}
		popOut = append(popOut, item)
	}
	out["population"] = popOut
	if score, ok := measureScore(scoring, scoreCounts); ok {
		out["measureScore"] = map[string]any{"value": score}
	}
	if len(g.Stratifier) > 0 && len(stratumPops) > 0 {
		var stratOut []any
		for si, strat := range g.Stratifier {
			sitem := map[string]any{}
			if strat.ID != "" {
				sitem["id"] = strat.ID
			}
			if code := strat.Code.code(); code != "" {
				sitem["code"] = conceptJSON(strat.Code)
			}
			var strata []any
			for key, popCounts := range stratumPops {
				if key.strat != si {
					continue
				}
				var spops []any
				for _, p := range pops {
					spops = append(spops, map[string]any{
						"code":  conceptJSON(p.def.Code),
						"count": popCounts[p.code],
					})
				}
				stratum := map[string]any{
					"value":      map[string]any{"text": key.value},
					"population": spops,
				}
				strata = append(strata, stratum)
			}
			if len(strata) > 0 {
				sitem["stratum"] = strata
			}
			stratOut = append(stratOut, sitem)
		}
		out["stratifier"] = stratOut
	}
	return out, contained, nil
}

func evalPopulation(ctx context.Context, e *Engine, pop fhirMeasurePopulation, env EvalContext) (bool, int, error) {
	expr := strings.TrimSpace(pop.Criteria.Expression)
	if expr == "" {
		return false, 0, errf("%w: population criteria expression is empty", ErrMeasure)
	}
	lang := pop.Criteria.Language
	if lang == "" {
		lang = "text/cql.identifier"
	}
	env.Language = lang
	vals, err := e.Eval(ctx, expr, env)
	if err != nil {
		return false, 0, errf("%w: population %s: %v", ErrMeasure, pop.Code.code(), err)
	}
	if len(vals) == 0 {
		return false, 0, nil
	}
	if len(vals) == 1 {
		if b := asBool(vals); b != nil {
			if *b {
				return true, 1, nil
			}
			return false, 0, nil
		}
	}
	return true, 1, nil
}

func populationCountMap(pops []popEval) map[string]int {
	out := map[string]int{}
	for _, p := range pops {
		out[p.code] = p.count
	}
	return out
}

func applyPopulationExclusions(counts map[string]int, scoring string) map[string]int {
	out := map[string]int{}
	for k, v := range counts {
		out[k] = v
	}
	get := func(code string) int {
		return out[code]
	}
	set := func(code string, n int) {
		if n < 0 {
			n = 0
		}
		out[code] = n
	}
	switch scoring {
	case "proportion", "ratio":
		den := get("denominator")
		denex := get("denominator-exclusion")
		denexcep := get("denominator-exception")
		num := get("numerator")
		numex := get("numerator-exclusion")
		if denex > 0 {
			set("denominator", den-denex)
			den = get("denominator")
		}
		if numex > 0 {
			set("numerator", num-numex)
			num = get("numerator")
		}
		if denexcep > 0 {
			adjust := denexcep
			if adjust > den-num {
				adjust = den - num
			}
			if adjust < 0 {
				adjust = 0
			}
			set("denominator", den-adjust)
		}
	}
	return out
}

func measureScore(scoring string, counts map[string]int) (float64, bool) {
	switch scoring {
	case "proportion", "ratio":
		den := counts["denominator"]
		if den <= 0 {
			return 0, false
		}
		return float64(counts["numerator"]) / float64(den), true
	case "cohort":
		n := counts["initial-population"]
		return float64(n), true
	case "continuous-variable":
		n := counts["measure-population"]
		if n <= 0 {
			return 0, false
		}
		return float64(n), true
	}
	if den := counts["denominator"]; den > 0 {
		return float64(counts["numerator"]) / float64(den), true
	}
	return 0, false
}

func conceptJSON(c fhirConcept) map[string]any {
	out := map[string]any{}
	if len(c.Coding) > 0 {
		var coding []any
		for _, cd := range c.Coding {
			item := map[string]any{}
			if cd.System != "" {
				item["system"] = cd.System
			}
			if cd.Code != "" {
				item["code"] = cd.Code
			}
			if len(item) > 0 {
				coding = append(coding, item)
			}
		}
		if len(coding) > 0 {
			out["coding"] = coding
		}
	}
	if c.Text != "" {
		out["text"] = c.Text
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func evalSupplementalData(ctx context.Context, e *Engine, m fhirMeasure, subjects []any, env EvalContext) ([]any, []any, error) {
	if len(m.SupplementalData) == 0 || len(subjects) == 0 {
		return nil, nil, nil
	}
	var contained []any
	var refs []any
	idx := 0
	for si, subject := range subjects {
		inIP, err := subjectInInitialPopulation(ctx, e, m, subject, env)
		if err != nil {
			return nil, nil, err
		}
		if !inIP {
			continue
		}
		env.Patient = subject
		for _, sd := range m.SupplementalData {
			expr := strings.TrimSpace(sd.Criteria.Expression)
			if expr == "" {
				continue
			}
			lang := sd.Criteria.Language
			if lang == "" {
				lang = "text/cql.identifier"
			}
			env.Language = lang
			vals, err := e.Eval(ctx, expr, env)
			if err != nil {
				return nil, nil, errf("%w: supplementalData: %v", ErrMeasure, err)
			}
			id := "sde-" + itoa(idx)
			if sd.ID != "" {
				id = sd.ID
				if len(subjects) > 1 {
					id = sd.ID + "-" + itoa(si)
				}
			}
			idx++
			obs := map[string]any{
				"resourceType": "Observation",
				"id":           id,
				"status":       "final",
			}
			if code := sd.Code.code(); code != "" || sd.Code.Text != "" {
				obs["code"] = conceptJSON(sd.Code)
			} else {
				text := sd.ID
				if text == "" {
					text = expr
				}
				obs["code"] = map[string]any{"text": text}
			}
			if ref := patientReference(subject); ref != "" {
				obs["subject"] = map[string]any{"reference": ref}
			}
			applyObservationValue(obs, vals)
			contained = append(contained, obs)
			refs = append(refs, map[string]any{"reference": "#" + id})
			for _, v := range vals {
				if r := patientReference(v); r != "" {
					refs = append(refs, map[string]any{"reference": r})
				}
			}
		}
	}
	return contained, refs, nil
}

func subjectInInitialPopulation(ctx context.Context, e *Engine, m fhirMeasure, subject any, env EvalContext) (bool, error) {
	hasIP := false
	env.Patient = subject
	for _, g := range m.Group {
		for _, p := range g.Population {
			if strings.ToLower(p.Code.code()) != "initial-population" {
				continue
			}
			hasIP = true
			ok, _, err := evalPopulation(ctx, e, p, env)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
	}
	return !hasIP, nil
}

func applyObservationValue(obs map[string]any, vals []any) {
	if len(vals) == 0 {
		return
	}
	if len(vals) > 1 {
		raw, err := json.Marshal(jsonifyCQL(vals))
		if err != nil {
			obs["valueString"] = fmt.Sprint(vals)
			return
		}
		obs["valueString"] = string(raw)
		return
	}
	v := unwrapPrimitive(vals[0])
	if r := patientReference(v); r != "" {
		obs["valueReference"] = map[string]any{"reference": r}
		return
	}
	switch x := v.(type) {
	case bool:
		obs["valueBoolean"] = x
	case int:
		obs["valueInteger"] = x
	case int32:
		obs["valueInteger"] = int64(x)
	case int64:
		obs["valueInteger"] = x
	case float64:
		obs["valueDecimal"] = x
	case time.Time:
		obs["valueDateTime"] = x.UTC().Format(time.RFC3339)
	case string:
		obs["valueString"] = x
	case Quantity:
		item := map[string]any{"value": x.Value}
		if x.Unit != "" {
			item["unit"] = x.Unit
		}
		obs["valueQuantity"] = item
	default:
		raw, err := json.Marshal(jsonifyCQL(v))
		if err != nil {
			obs["valueString"] = fmt.Sprint(v)
			return
		}
		obs["valueString"] = string(raw)
	}
}

func jsonifyCQL(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case Quantity:
		return map[string]any{"value": x.Value, "unit": x.Unit}
	case Interval:
		return map[string]any{"low": jsonifyCQL(x.Low), "high": jsonifyCQL(x.High), "lowClosed": x.LowClosed, "highClosed": x.HighClosed}
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = jsonifyCQL(el)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, el := range x {
			out[k] = jsonifyCQL(el)
		}
		return out
	default:
		return unwrapPrimitive(v)
	}
}

// ListStorePatients loads Patient resources from a ResourceStore for summary evaluation.
func ListStorePatients(ctx context.Context, resources interface {
	ListIDs(ctx context.Context, resourceType string, limit, offset int) ([]string, error)
	Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
}) ([]any, error) {
	if resources == nil {
		return nil, errf("%w: patient store is unavailable", ErrMeasure)
	}
	ids, err := resources.ListIDs(ctx, "Patient", 10000, 0)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, id := range ids {
		env, err := resources.Read(ctx, "Patient", id)
		if err != nil {
			return nil, err
		}
		if env == nil {
			return nil, errf("%w: Patient %s was not found", ErrMeasure, id)
		}
		out = append(out, env)
	}
	return out, nil
}

// MeasureLibraryCanonicals returns Measure.library canonicals.
func MeasureLibraryCanonicals(env *types.ResourceEnvelope) []string {
	if env == nil || len(env.JSON) == 0 {
		return nil
	}
	m, err := parseMeasure(env)
	if err != nil {
		return nil
	}
	return m.Library
}
