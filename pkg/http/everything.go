package http

import (
	"context"
	"encoding/json"
	"net/http"
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
	if h.cfg.CapabilitySource != nil && len(query.Types) == 0 {
		snapshot := h.cfg.CapabilitySource.CapabilitySnapshot()
		for _, res := range snapshot.Resources {
			if res.ResourceType != "" {
				query.Types = append(query.Types, res.ResourceType)
			}
		}
	}
	envelopes, err := h.everything(r.Context(), route.id, query)
	if err != nil {
		writeError(w, err)
		return
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
		entries = append(entries, search.BundleEntry{
			FullURL:  locationURL(h.cfg.BasePath, env.ResourceType, env.ID),
			Resource: env,
			Mode:     "match",
		})
	}
	total := len(entries)
	data, err := marshalSearchBundle(&search.SearchBundle{
		ResourceType: "Bundle",
		Total:        &total,
		Entries:      entries,
	})
	if err != nil {
		writeError(w, invalidRequest("build $everything bundle", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func (h *handler) everything(ctx context.Context, patientID string, q core.EverythingQuery) ([]*types.ResourceEnvelope, error) {
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
	return nil, notImplementedEndpoint("Patient/$everything")
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
	if raw := strings.TrimSpace(q.Get("_count")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return out, invalidRequest("invalid _count parameter", err)
		}
		out.Count = n
	}
	if out.Since.IsZero() && q.Get("start") != "" {
		if ts, err := time.Parse("2006-01-02", q.Get("start")); err == nil {
			out.Since = ts.UTC()
		}
	}
	return out, nil
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
