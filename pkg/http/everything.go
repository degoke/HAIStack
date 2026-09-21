package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func (h *handler) handleEverything(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if route.resourceType != "Patient" || route.id == "" {
		h.handleCustomOperation(w, r, route)
		return
	}
	if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	query, err := parseEverythingQuery(r)
	if err != nil {
		writeError(w, err)
		return
	}
	pageSize := query.Count
	if pageSize <= 0 {
		pageSize = 1000
	}
	query.Count = pageSize + 1
	envelopes, err := h.everything(r.Context(), route.id, query)
	if err != nil {
		writeError(w, err)
		return
	}
	hasMore := len(envelopes) > pageSize
	if hasMore {
		envelopes = envelopes[:pageSize]
	}
	entries := make([]search.BundleEntry, 0, len(envelopes))
	for _, env := range envelopes {
		if env == nil {
			continue
		}
		if err := h.enforcePatientScopeOnEnvelope(r.Context(), env); err != nil {
			continue
		}
		if err := h.enforceScopeFiltersOnEnvelope(r.Context(), env.ResourceType, smart.OpRead, env); err != nil {
			continue
		}
		mode := "include"
		if env.ResourceType == "Patient" && env.ID == route.id {
			mode = "match"
		}
		entries = append(entries, search.BundleEntry{
			FullURL:  locationURL(h.cfg.BasePath, env.ResourceType, env.ID),
			Resource: env,
			Mode:     mode,
		})
	}
	total := (*int)(nil)
	if !hasMore && query.Offset == 0 {
		n := len(entries)
		total = &n
	}
	links := map[string]string{
		"self": everythingPageURL(h.cfg.BasePath, route.id, r.URL.Query(), query.Offset, pageSize),
	}
	if hasMore {
		links["next"] = everythingPageURL(h.cfg.BasePath, route.id, r.URL.Query(), query.Offset+pageSize, pageSize)
	}
	data, err := marshalSearchBundle(&search.SearchBundle{
		ResourceType: "Bundle",
		Total:        total,
		Entries:      entries,
		Links:        links,
	})
	if err != nil {
		writeError(w, invalidRequest("build $everything bundle", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func everythingPageURL(basePath, patientID string, params url.Values, offset, count int) string {
	query := url.Values{}
	for key, values := range params {
		if key == "_offset" || key == "_count" {
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	if offset > 0 {
		query.Set("_offset", strconv.Itoa(offset))
	}
	query.Set("_count", strconv.Itoa(count))
	path := strings.TrimSuffix(basePath, "/") + "/Patient/" + patientID + "/$everything"
	encoded := query.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}

func (h *handler) everything(ctx context.Context, patientID string, q core.EverythingQuery) ([]*types.ResourceEnvelope, error) {
	hasEverything := resourceServiceHasEverything(h.cfg.ResourceService)
	// Prefer compartment search when both search and Everything are wired so
	// production does not list every ID of every type. Tests that inject a
	// fake SearchService plus OperationService keep the operation fallback.
	if h.cfg.SearchService != nil && hasEverything {
		return h.everythingBySearch(ctx, patientID, q)
	}
	if svc, ok := h.cfg.ResourceService.(interface {
		Everything(context.Context, string, core.EverythingQuery) ([]*types.ResourceEnvelope, error)
	}); ok {
		return svc.Everything(ctx, patientID, q)
	}
	if h.cfg.OperationService != nil {
		result, err := h.cfg.OperationService.Execute(ctx, OperationRequest{
			ResourceType: "Patient",
			ID:           patientID,
			Operation:    "$everything",
		})
		if err != nil {
			return nil, err
		}
		return envelopesFromEverythingBundle(result)
	}
	if h.cfg.SearchService != nil {
		return h.everythingBySearch(ctx, patientID, q)
	}
	return nil, notImplementedEndpoint("Patient/$everything")
}

func (h *handler) everythingBySearch(ctx context.Context, patientID string, q core.EverythingQuery) ([]*types.ResourceEnvelope, error) {
	patient, err := h.cfg.ResourceService.Read(ctx, "Patient", patientID)
	if err != nil {
		return nil, err
	}
	typesToScan := q.Types
	if len(typesToScan) == 0 {
		typesToScan = core.DefaultEverythingTypes
	}
	limit := q.Count
	if limit <= 0 {
		limit = 1001
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	out := make([]*types.ResourceEnvelope, 0, 8)
	appendEnv := func(env *types.ResourceEnvelope) bool {
		if env == nil {
			return true
		}
		if !resourceMatchesEverythingHTTP(env, patientID, q) {
			return true
		}
		out = append(out, env)
		return len(out) < offset+limit
	}
	if !appendEnv(patient) {
		return paginateEverythingHTTP(out, offset, limit), nil
	}
	for _, resourceType := range typesToScan {
		if resourceType == "" || resourceType == "Patient" {
			continue
		}
		paramsList := core.PatientCompartmentSearchParams(resourceType)
		if len(paramsList) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, param := range paramsList {
			if err := h.searchEverythingParam(ctx, patientID, resourceType, param, q, seen, appendEnv); err != nil {
				return nil, err
			}
			if len(out) >= offset+limit {
				return paginateEverythingHTTP(out, offset, limit), nil
			}
		}
	}
	return paginateEverythingHTTP(out, offset, limit), nil
}

const everythingSearchPageSize = 1000

func (h *handler) searchEverythingParam(
	ctx context.Context,
	patientID, resourceType, param string,
	q core.EverythingQuery,
	seen map[string]bool,
	appendEnv func(*types.ResourceEnvelope) bool,
) error {
	pageOffset := 0
	for {
		params := url.Values{}
		params.Set(param, "Patient/"+patientID)
		params.Set("_count", strconv.Itoa(everythingSearchPageSize))
		if pageOffset > 0 {
			params.Set("_offset", strconv.Itoa(pageOffset))
		}
		if !q.Since.IsZero() {
			params.Set("_lastUpdated", "ge"+q.Since.UTC().Format(time.RFC3339Nano))
		}
		bundle, err := h.cfg.SearchService.SearchBundle(ctx, resourceType, params)
		if err != nil {
			return err
		}
		if bundle == nil {
			return nil
		}
		matchCount := 0
		added := 0
		for _, entry := range bundle.Entries {
			if entry.Resource == nil || entry.Mode == "include" {
				continue
			}
			matchCount++
			key := entry.Resource.ResourceType + "/" + entry.Resource.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			if !appendEnv(entry.Resource) {
				return nil
			}
		}
		nextOffset, hasNext := everythingSearchNextOffset(bundle, pageOffset, matchCount)
		if !hasNext || added == 0 {
			return nil
		}
		if nextOffset <= pageOffset {
			nextOffset = pageOffset + matchCount
		}
		if nextOffset <= pageOffset {
			return nil
		}
		pageOffset = nextOffset
	}
}

func everythingSearchNextOffset(bundle *search.SearchBundle, pageOffset, matchCount int) (int, bool) {
	if bundle == nil {
		return 0, false
	}
	if next := strings.TrimSpace(bundle.Links["next"]); next != "" {
		if u, err := url.Parse(next); err == nil {
			if raw := strings.TrimSpace(u.Query().Get("_offset")); raw != "" {
				if n, err := strconv.Atoi(raw); err == nil && n > pageOffset {
					return n, true
				}
			}
		}
		return pageOffset + matchCount, matchCount > 0
	}
	if matchCount >= everythingSearchPageSize {
		return pageOffset + matchCount, true
	}
	return 0, false
}

func resourceMatchesEverythingHTTP(env *types.ResourceEnvelope, patientID string, q core.EverythingQuery) bool {
	if env == nil {
		return false
	}
	if env.ResourceType == "Patient" {
		return env.ID == patientID && everythingSinceOK(env, q) && everythingCareDateOK(env, q)
	}
	return everythingSinceOK(env, q) && everythingCareDateOK(env, q)
}

func everythingSinceOK(env *types.ResourceEnvelope, q core.EverythingQuery) bool {
	if q.Since.IsZero() || env.LastUpdated.IsZero() {
		return true
	}
	return !env.LastUpdated.Before(q.Since)
}

func everythingCareDateOK(env *types.ResourceEnvelope, q core.EverythingQuery) bool {
	if q.Start.IsZero() && q.End.IsZero() {
		return true
	}
	dates := extractCareDates(env)
	if len(dates) == 0 {
		return true
	}
	for _, ts := range dates {
		if !q.Start.IsZero() && ts.Before(q.Start) {
			continue
		}
		if !q.End.IsZero() && ts.After(q.End) {
			continue
		}
		return true
	}
	return false
}

func extractCareDates(env *types.ResourceEnvelope) []time.Time {
	if env == nil || len(env.JSON) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(env.JSON, &obj); err != nil {
		return nil
	}
	var out []time.Time
	collectHTTPCareDates(obj, &out)
	return out
}

func collectHTTPCareDates(value any, out *[]time.Time) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			switch key {
			case "effectiveDateTime", "issued", "date", "authoredOn", "recordedDate",
				"occurrenceDateTime", "onsetDateTime", "performedDateTime", "created":
				if ts, ok := parseHTTPCareDate(child); ok {
					*out = append(*out, ts)
				}
			case "effectivePeriod", "period", "onsetPeriod", "performedPeriod", "occurrencePeriod", "billablePeriod":
				collectHTTPPeriodDates(child, out)
			case "meta", "text", "extension", "contained":
				continue
			default:
				collectHTTPCareDates(child, out)
			}
		}
	case []any:
		for _, child := range node {
			collectHTTPCareDates(child, out)
		}
	}
}

func collectHTTPPeriodDates(value any, out *[]time.Time) {
	switch node := value.(type) {
	case map[string]any:
		if ts, ok := parseHTTPCareDate(node["start"]); ok {
			*out = append(*out, ts)
		}
		if ts, ok := parseHTTPCareDate(node["end"]); ok {
			*out = append(*out, ts)
		}
	case []any:
		for _, child := range node {
			collectHTTPPeriodDates(child, out)
		}
	}
}

func parseHTTPCareDate(value any) (time.Time, bool) {
	s, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

func paginateEverythingHTTP(envs []*types.ResourceEnvelope, offset, limit int) []*types.ResourceEnvelope {
	if offset >= len(envs) {
		return nil
	}
	envs = envs[offset:]
	if limit > 0 && len(envs) > limit {
		return envs[:limit]
	}
	return envs
}

func parseEverythingQuery(r *http.Request) (core.EverythingQuery, error) {
	q := r.URL.Query()
	out := core.EverythingQuery{
		Types: parseCSVParam(q.Get("_type")),
	}
	if raw := strings.TrimSpace(q.Get("_since")); raw != "" {
		ts, err := parseFHIRInstant(raw)
		if err != nil {
			return out, invalidRequest("invalid _since parameter", err)
		}
		out.Since = ts
	}
	if raw := strings.TrimSpace(q.Get("start")); raw != "" {
		ts, err := parseEverythingDate(raw)
		if err != nil {
			return out, invalidRequest("invalid start parameter", err)
		}
		out.Start = ts
	}
	if raw := strings.TrimSpace(q.Get("end")); raw != "" {
		ts, err := parseEverythingDate(raw)
		if err != nil {
			return out, invalidRequest("invalid end parameter", err)
		}
		out.End = ts
	}
	if raw := strings.TrimSpace(q.Get("_count")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return out, invalidRequest("invalid _count parameter", err)
		}
		out.Count = n
	}
	if raw := strings.TrimSpace(q.Get("_offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return out, invalidRequest("invalid _offset parameter", err)
		}
		out.Offset = n
	}
	return out, nil
}

func parseEverythingDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if ts, err := parseFHIRInstant(raw); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse("2006-01-02", raw); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, invalidRequest("invalid date "+raw, nil)
}

func envelopesFromEverythingBundle(bundle *types.ResourceEnvelope) ([]*types.ResourceEnvelope, error) {
	if bundle == nil {
		return nil, nil
	}
	if bundle.ResourceType != "Bundle" {
		return []*types.ResourceEnvelope{bundle}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(bundle.JSON, &obj); err != nil {
		return nil, invalidRequest("parse $everything bundle", err)
	}
	raw, _ := obj["entry"].([]any)
	out := make([]*types.ResourceEnvelope, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		res, ok := entry["resource"].(map[string]any)
		if !ok {
			continue
		}
		data, err := json.Marshal(res)
		if err != nil {
			continue
		}
		rt, _ := res["resourceType"].(string)
		id, _ := res["id"].(string)
		out = append(out, &types.ResourceEnvelope{ResourceType: rt, ID: id, JSON: data})
	}
	return out, nil
}

func resourceServiceHasEverything(svc ResourceService) bool {
	if svc == nil {
		return false
	}
	_, ok := svc.(interface {
		Everything(context.Context, string, core.EverythingQuery) ([]*types.ResourceEnvelope, error)
	})
	return ok
}
