package http

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/cql"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// CoreMeasureService implements Measure/$evaluate-measure using pkg/cql.
type CoreMeasureService struct {
	Engine    *cql.Engine
	Libraries cql.LibraryResolver
	Resources store.ResourceStore
	Retriever cql.Retriever
}

func (s CoreMeasureService) EvaluateMeasure(ctx context.Context, req MeasureEvaluateRequest) (*types.ResourceEnvelope, error) {
	if s.Engine == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "CQL measure evaluation is unavailable"}
	}
	params, err := parseEvaluateMeasureInput(req)
	if err != nil {
		return nil, err
	}
	measure, err := s.loadMeasure(ctx, req.ID, params.measureCanonical)
	if err != nil {
		return nil, err
	}
	libs, err := s.loadLibraries(ctx, measure)
	if err != nil {
		return nil, err
	}
	periodStart, err := parseMeasureDate(params.periodStart, "periodStart")
	if err != nil {
		return nil, err
	}
	periodEnd, err := parseMeasureDate(params.periodEnd, "periodEnd")
	if err != nil {
		return nil, err
	}
	mreq := cql.MeasureRequest{
		Measure:     measure,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		ReportType:  params.reportType,
		Subject:     params.subject,
		Libraries:   libs,
		Retriever:   s.Retriever,
		Parameters:  params.extra,
	}
	reportType := strings.ToLower(strings.TrimSpace(params.reportType))
	isGroup := strings.Contains(strings.ToLower(params.subject), "group/")
	wantIndividual := reportType == "individual" || reportType == "subject" || (reportType == "" && params.subject != "" && !isGroup)
	if isGroup {
		patients, err := s.loadGroupMembers(ctx, params.subject)
		if err != nil {
			return nil, err
		}
		mreq.Patients = patients
		switch {
		case wantIndividual && len(patients) == 1:
			mreq.Patient = patients[0]
			mreq.ReportType = "individual"
		case wantIndividual:
			mreq.ReportType = "summary"
		case mreq.ReportType == "":
			mreq.ReportType = "summary"
		}
	} else if wantIndividual {
		patient, err := s.loadPatient(ctx, params.subject)
		if err != nil {
			return nil, err
		}
		mreq.Patient = patient
		if mreq.ReportType == "" {
			mreq.ReportType = "individual"
		}
	} else {
		patients, err := cql.ListStorePatients(ctx, s.Resources)
		if err != nil {
			return nil, mapMeasureError(err)
		}
		mreq.Patients = patients
		if mreq.ReportType == "" {
			mreq.ReportType = "summary"
		}
	}
	report, err := s.Engine.EvaluateMeasure(ctx, mreq)
	if err != nil {
		return nil, mapMeasureError(err)
	}
	return report, nil
}

type evaluateMeasureParams struct {
	periodStart      string
	periodEnd        string
	reportType       string
	subject          string
	measureCanonical string
	extra            map[string]any
}

type measureParameter struct {
	Name           string   `json:"name"`
	ValueDate      string   `json:"valueDate"`
	ValueDateTime  string   `json:"valueDateTime"`
	ValueCode      string   `json:"valueCode"`
	ValueString    string   `json:"valueString"`
	ValueCanonical string   `json:"valueCanonical"`
	ValueInteger   *int64   `json:"valueInteger"`
	ValueBoolean   *bool    `json:"valueBoolean"`
	ValueDecimal   *float64 `json:"valueDecimal"`
	ValueQuantity  *struct {
		Value float64 `json:"value"`
		Unit  string  `json:"unit"`
		Code  string  `json:"code"`
	} `json:"valueQuantity"`
}

func parseEvaluateMeasureInput(req MeasureEvaluateRequest) (evaluateMeasureParams, error) {
	out := evaluateMeasureParams{
		periodStart:      strings.TrimSpace(req.Query.Get("periodStart")),
		periodEnd:        strings.TrimSpace(req.Query.Get("periodEnd")),
		reportType:       strings.TrimSpace(req.Query.Get("reportType")),
		subject:          strings.TrimSpace(req.Query.Get("subject")),
		measureCanonical: strings.TrimSpace(req.Query.Get("measure")),
	}
	if len(req.Body) == 0 {
		if out.periodStart == "" || out.periodEnd == "" {
			return evaluateMeasureParams{}, invalidRequest("periodStart and periodEnd are required", nil)
		}
		return out, nil
	}
	var peek struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(req.Body, &peek); err != nil {
		return evaluateMeasureParams{}, invalidRequest("parse $evaluate-measure input", err)
	}
	if peek.ResourceType != "" && peek.ResourceType != "Parameters" {
		return evaluateMeasureParams{}, invalidRequest("Measure/$evaluate-measure body must be Parameters", nil)
	}
	var body struct {
		Parameter []measureParameter `json:"parameter"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return evaluateMeasureParams{}, invalidRequest("parse Parameters", err)
	}
	for _, p := range body.Parameter {
		switch p.Name {
		case "periodStart":
			if v := firstMeasureParam(p.ValueDate, p.ValueDateTime); v != "" {
				out.periodStart = v
			}
		case "periodEnd":
			if v := firstMeasureParam(p.ValueDate, p.ValueDateTime); v != "" {
				out.periodEnd = v
			}
		case "reportType":
			if v := firstMeasureParam(p.ValueCode, p.ValueString); v != "" {
				out.reportType = v
			}
		case "subject":
			if v := firstMeasureParam(p.ValueString, p.ValueCanonical); v != "" {
				out.subject = v
			}
		case "measure":
			if v := firstMeasureParam(p.ValueCanonical, p.ValueString); v != "" {
				out.measureCanonical = v
			}
		default:
			if v, ok := parameterCQLValue(p); ok {
				if out.extra == nil {
					out.extra = map[string]any{}
				}
				out.extra[p.Name] = v
			}
		}
	}
	if out.periodStart == "" || out.periodEnd == "" {
		return evaluateMeasureParams{}, invalidRequest("periodStart and periodEnd are required", nil)
	}
	return out, nil
}

func parameterCQLValue(p measureParameter) (any, bool) {
	if p.ValueInteger != nil {
		return *p.ValueInteger, true
	}
	if p.ValueBoolean != nil {
		return *p.ValueBoolean, true
	}
	if p.ValueDecimal != nil {
		return *p.ValueDecimal, true
	}
	if p.ValueQuantity != nil {
		unit := p.ValueQuantity.Unit
		if unit == "" {
			unit = p.ValueQuantity.Code
		}
		return cql.Quantity{Value: p.ValueQuantity.Value, Unit: unit}, true
	}
	if v := firstMeasureParam(p.ValueDateTime, p.ValueDate); v != "" {
		if tm, err := parseMeasureDate(v, p.Name); err == nil {
			return tm, true
		}
		return v, true
	}
	if v := firstMeasureParam(p.ValueString, p.ValueCode, p.ValueCanonical); v != "" {
		return v, true
	}
	return nil, false
}

func firstMeasureParam(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseMeasureDate(raw, name string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, invalidRequest(name+" is required", nil)
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02"} {
		if tm, err := time.Parse(layout, raw); err == nil {
			return tm.UTC(), nil
		}
	}
	return time.Time{}, invalidRequest("invalid "+name, nil)
}

func (s CoreMeasureService) loadMeasure(ctx context.Context, id, canonical string) (*types.ResourceEnvelope, error) {
	if s.Resources == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "resource store is unavailable"}
	}
	if id != "" {
		env, err := s.Resources.Read(ctx, "Measure", id)
		if err != nil {
			return nil, err
		}
		if env == nil {
			return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "Measure " + id + " was not found"}
		}
		return env, nil
	}
	if canonical == "" {
		return nil, invalidRequest("measure id or measure canonical is required", nil)
	}
	ids, err := s.Resources.ListIDs(ctx, "Measure", 10000, 0)
	if err != nil {
		return nil, err
	}
	want, version := splitMeasureCanonical(canonical)
	for _, mid := range ids {
		env, err := s.Resources.Read(ctx, "Measure", mid)
		if err != nil || env == nil {
			continue
		}
		var meta struct {
			URL     string `json:"url"`
			Version string `json:"version"`
		}
		if json.Unmarshal(env.JSON, &meta) != nil {
			continue
		}
		if meta.URL == want && (version == "" || meta.Version == version) {
			return env, nil
		}
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "Measure " + canonical + " was not found"}
}

func splitMeasureCanonical(canonical string) (url, version string) {
	parts := strings.SplitN(canonical, "|", 2)
	url = parts[0]
	if len(parts) == 2 {
		version = parts[1]
	}
	return url, version
}

func (s CoreMeasureService) loadLibraries(ctx context.Context, measure *types.ResourceEnvelope) ([]*cql.Library, error) {
	var out []*cql.Library
	for _, ref := range cql.MeasureLibraryCanonicals(measure) {
		if ref == "" {
			continue
		}
		if s.Libraries == nil {
			return nil, mapMeasureError(cql.ErrLibraryNotFound)
		}
		lib, err := s.Libraries.Resolve(ctx, ref)
		if err != nil {
			return nil, mapMeasureError(err)
		}
		out = append(out, lib)
	}
	if len(out) == 0 {
		return nil, invalidRequest("Measure.library is required for $evaluate-measure", nil)
	}
	return out, nil
}

func (s CoreMeasureService) loadPatient(ctx context.Context, subject string) (*types.ResourceEnvelope, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, invalidRequest("subject is required for an individual MeasureReport", nil)
	}
	id := subject
	if i := strings.LastIndex(subject, "/"); i >= 0 {
		id = subject[i+1:]
	}
	if s.Resources == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "resource store is unavailable"}
	}
	env, err := s.Resources.Read(ctx, "Patient", id)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "Patient " + id + " was not found"}
	}
	return env, nil
}

func (s CoreMeasureService) loadGroupMembers(ctx context.Context, subject string) ([]any, error) {
	id := subject
	if i := strings.LastIndex(subject, "/"); i >= 0 {
		id = subject[i+1:]
	}
	if s.Resources == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "resource store is unavailable"}
	}
	env, err := s.Resources.Read(ctx, "Group", id)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "Group " + id + " was not found"}
	}
	var g struct {
		Member []struct {
			Entity struct {
				Reference string `json:"reference"`
			} `json:"entity"`
		} `json:"member"`
	}
	if err := json.Unmarshal(env.JSON, &g); err != nil {
		return nil, invalidRequest("decode Group", err)
	}
	var out []any
	for _, m := range g.Member {
		ref := strings.TrimSpace(m.Entity.Reference)
		if ref == "" {
			continue
		}
		pid := ref
		if i := strings.LastIndex(ref, "/"); i >= 0 {
			pid = ref[i+1:]
		}
		pat, err := s.Resources.Read(ctx, "Patient", pid)
		if err != nil || pat == nil {
			continue
		}
		out = append(out, pat)
	}
	return out, nil
}

func mapMeasureError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, cql.ErrLibraryNotFound), errors.Is(err, cql.ErrExpressionNotFound):
		return &core.ServiceError{Kind: core.ErrorKindNotFound, Message: err.Error(), Cause: err}
	case errors.Is(err, cql.ErrMissingContext), errors.Is(err, cql.ErrMeasure), errors.Is(err, cql.ErrEmptyExpression):
		return &core.ServiceError{Kind: core.ErrorKindInvalid, Message: err.Error(), Cause: err}
	case errors.Is(err, cql.ErrUnsupported), errors.Is(err, cql.ErrEngineUnavailable):
		return &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: err.Error(), Cause: err}
	default:
		return err
	}
}
