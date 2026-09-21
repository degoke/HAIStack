package cql

import (
	"context"
	"encoding/json"
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
	periodStart, periodEnd := req.PeriodStart, req.PeriodEnd
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
		HighClosed: false,
	}
	subjects, err := measureSubjects(req, reportType)
	if err != nil {
		return nil, err
	}
	env := EvalContext{
		Parameters: params,
		Libraries:  req.Libraries,
		Now:        e.clock(),
		Retriever:  req.Retriever,
	}
	return buildMeasureReport(ctx, e, m, reportType, periodStart, periodEnd, subjects, env)
}

func measureSubjects(req MeasureRequest, reportType string) ([]any, error) {
	if reportType == "individual" {
		if req.Patient != nil {
			return []any{req.Patient}, nil
		}
		return nil, errf("%w: individual report requires a subject Patient", ErrMeasure)
	}
	if len(req.Patients) == 0 {
		return nil, errf("%w: summary report requires a patient population", ErrMeasure)
	}
	return req.Patients, nil
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
	if reportType == "individual" && len(subjects) == 1 {
		if ref := patientReference(subjects[0]); ref != "" {
			report["subject"] = map[string]any{"reference": ref}
		}
	}
	var groups []any
	for gi, g := range m.Group {
		groupOut, err := evalMeasureGroup(ctx, e, g, gi, scoring, reportType, subjects, env)
		if err != nil {
			return nil, err
		}
		groups = append(groups, groupOut)
	}
	report["group"] = groups
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

func evalMeasureGroup(ctx context.Context, e *Engine, g fhirMeasureGroup, index int, scoring, reportType string, subjects []any, env EvalContext) (map[string]any, error) {
	pops := make([]popEval, len(g.Population))
	for i, p := range g.Population {
		pops[i] = popEval{def: p, code: strings.ToLower(p.Code.code())}
	}
	for _, subject := range subjects {
		env.Patient = subject
		for i := range pops {
			ok, n, err := evalPopulation(ctx, e, pops[i].def, env)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if reportType == "individual" {
				pops[i].count += n
			} else {
				pops[i].count++
			}
			if reportType == "subject-list" {
				if ref := patientReference(subject); ref != "" {
					pops[i].subjects = append(pops[i].subjects, ref)
				}
			}
		}
	}
	applyPopulationExclusions(pops, scoring)
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
	counts := map[string]int{}
	for _, p := range pops {
		item := map[string]any{
			"code":  conceptJSON(p.def.Code),
			"count": p.count,
		}
		if p.def.ID != "" {
			item["id"] = p.def.ID
		}
		if reportType == "subject-list" && len(p.subjects) > 0 {
			item["subjectResults"] = map[string]any{
				"display": strings.Join(p.subjects, ", "),
			}
		}
		popOut = append(popOut, item)
		counts[p.code] = p.count
	}
	out["population"] = popOut
	if score, ok := measureScore(scoring, counts); ok {
		out["measureScore"] = map[string]any{"value": score}
	}
	return out, nil
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
	return true, len(vals), nil
}

func applyPopulationExclusions(pops []popEval, scoring string) {
	get := func(code string) int {
		for _, p := range pops {
			if p.code == code {
				return p.count
			}
		}
		return 0
	}
	set := func(code string, n int) {
		if n < 0 {
			n = 0
		}
		for i := range pops {
			if pops[i].code == code {
				pops[i].count = n
			}
		}
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
		if err != nil || env == nil {
			continue
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
