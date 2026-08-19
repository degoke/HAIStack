package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type preconditionError struct {
	message string
}

func (e *preconditionError) Error() string {
	return e.message
}

func preconditionFailed(message string) error {
	return &preconditionError{message: message}
}

var conditionalControlParams = map[string]bool{
	"_format":        true,
	"_pretty":        true,
	"_summary":       true,
	"_elements":      true,
	"_count":         true,
	"_offset":        true,
	"_sort":          true,
	"_include":       true,
	"_revinclude":    true,
	"_contained":     true,
	"_containedType": true,
}

func conditionalParamsFromRequest(r *http.Request) (url.Values, error) {
	if r == nil {
		return nil, nil
	}
	if r.Method == http.MethodPost {
		if header := strings.TrimSpace(r.Header.Get("If-None-Exist")); header != "" {
			parsed, err := url.ParseQuery(header)
			if err != nil {
				return nil, invalidRequest("parse If-None-Exist", err)
			}
			filtered := filterConditionalParams(parsed)
			if filtered == nil {
				return nil, invalidRequest("If-None-Exist must contain search criteria", nil)
			}
			return filtered, nil
		}
	}
	return filterConditionalParams(r.URL.Query()), nil
}

func filterConditionalParams(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	out := url.Values{}
	for key, vals := range values {
		base := strings.SplitN(key, ":", 2)[0]
		if strings.HasPrefix(base, "_") && conditionalControlParams[base] {
			continue
		}
		for _, v := range vals {
			out.Add(key, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *handler) resolveConditionalMatches(ctx context.Context, resourceType string, params url.Values) ([]*types.ResourceEnvelope, int, error) {
	if h.cfg.SearchService == nil {
		return nil, 0, unsupportedEndpoint(resourceType)
	}
	var bundle *search.SearchBundle
	var err error
	if _, tenant, ok := identityFromContext(ctx); ok && tenant.PatientScope != "" {
		scoped, ok := h.cfg.SearchService.(PatientScopedSearchService)
		if !ok {
			return nil, 0, notImplementedEndpoint("patient-scoped conditional search")
		}
		bundle, err = scoped.SearchBundleForPatient(ctx, resourceType, tenant.PatientScope, params)
	} else {
		bundle, err = h.cfg.SearchService.SearchBundle(ctx, resourceType, params)
	}
	if err != nil {
		return nil, 0, err
	}
	if bundle == nil {
		return nil, 0, nil
	}
	matches := make([]*types.ResourceEnvelope, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		if entry.Resource != nil {
			matches = append(matches, entry.Resource)
		}
	}
	total := len(matches)
	if bundle.Total != nil && *bundle.Total >= 0 {
		total = *bundle.Total
	}
	return matches, total, nil
}

func (h *handler) handleConditionalCreate(w http.ResponseWriter, r *http.Request, resourceType string, params url.Values) {
	h.conditionalMu.Lock()
	defer h.conditionalMu.Unlock()
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	envelope, err := parseResourceBody(h.cfg.Codec, resourceType, r.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, err)
		return
	}

	matches, total, err := h.resolveConditionalMatches(r.Context(), resourceType, params)
	if err != nil {
		writeError(w, err)
		return
	}
	switch total {
	case 0:
		created, err := h.cfg.ResourceService.Create(r.Context(), envelope)
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusCreated, created, resourceHeaders(h.cfg.BasePath, created))
	case 1:
		if len(matches) != 1 || matches[0] == nil {
			writeError(w, invalidRequest("conditional search reported one match without returning it", nil))
			return
		}
		if err := h.authorizeRead(r.Context(), resourceType, matches[0].ID); err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, matches[0], resourceHeaders(h.cfg.BasePath, matches[0]))
	default:
		writeError(w, preconditionFailed(fmt.Sprintf("conditional create matched %d resources", total)))
	}
}

func (h *handler) handleConditionalUpdate(w http.ResponseWriter, r *http.Request, resourceType string, params url.Values) {
	h.conditionalMu.Lock()
	defer h.conditionalMu.Unlock()
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	envelope, err := parseResourceBody(h.cfg.Codec, resourceType, r.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, err)
		return
	}

	matches, total, err := h.resolveConditionalMatches(r.Context(), resourceType, params)
	if err != nil {
		writeError(w, err)
		return
	}
	switch total {
	case 0:
		if strings.TrimSpace(r.Header.Get("If-Match")) != "" {
			writeError(w, preconditionFailed("conditional update matched no resource for If-Match"))
			return
		}
		if err := h.authorizeWrite(r.Context(), "create", resourceType, envelope.ID); err != nil {
			writeError(w, err)
			return
		}
		created, err := h.cfg.ResourceService.Create(r.Context(), envelope)
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusCreated, created, resourceHeaders(h.cfg.BasePath, created))
	case 1:
		if len(matches) != 1 || matches[0] == nil {
			writeError(w, invalidRequest("conditional search reported one match without returning it", nil))
			return
		}
		if envelope.ID != "" && envelope.ID != matches[0].ID {
			writeError(w, idMismatch(matches[0].ID, envelope.ID))
			return
		}
		if err := h.authorizeWrite(r.Context(), "update", resourceType, matches[0].ID); err != nil {
			writeError(w, err)
			return
		}
		envelope.ID = matches[0].ID
		updated, err := h.updateConditionalMatch(r, envelope, matches[0])
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, updated, nil)
	default:
		writeError(w, preconditionFailed(fmt.Sprintf("conditional update matched %d resources", total)))
	}
}

func (h *handler) handleConditionalDelete(w http.ResponseWriter, r *http.Request, resourceType string, params url.Values) {
	h.conditionalMu.Lock()
	defer h.conditionalMu.Unlock()
	matches, total, err := h.resolveConditionalMatches(r.Context(), resourceType, params)
	if err != nil {
		writeError(w, err)
		return
	}
	switch total {
	case 0:
		// Conditional DELETE with no matches is an idempotent no-op.
		writeNoContent(w)
	case 1:
		if len(matches) != 1 || matches[0] == nil {
			writeError(w, invalidRequest("conditional search reported one match without returning it", nil))
			return
		}
		if err := h.authorizeWrite(r.Context(), "delete", resourceType, matches[0].ID); err != nil {
			writeError(w, err)
			return
		}
		if err := h.deleteConditionalMatch(r, resourceType, matches[0]); err != nil {
			writeError(w, err)
			return
		}
		writeNoContent(w)
	default:
		writeError(w, preconditionFailed(fmt.Sprintf("conditional delete matched %d resources", total)))
	}
}

func (h *handler) updateConditionalMatch(r *http.Request, envelope, current *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" {
		conditional, ok := h.cfg.ResourceService.(ConditionalResourceService)
		if !ok {
			return nil, notImplementedEndpoint("atomic If-Match update")
		}
		expected, err := h.resolveIfMatchVersion(r, current)
		if err != nil {
			return nil, err
		}
		return conditional.UpdateIfMatch(r.Context(), envelope, expected)
	}
	return h.cfg.ResourceService.Update(r.Context(), envelope)
}

func (h *handler) deleteConditionalMatch(r *http.Request, resourceType string, current *types.ResourceEnvelope) error {
	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" {
		conditional, ok := h.cfg.ResourceService.(ConditionalResourceService)
		if !ok {
			return notImplementedEndpoint("atomic If-Match delete")
		}
		expected, err := h.resolveIfMatchVersion(r, current)
		if err != nil {
			return err
		}
		return conditional.DeleteIfMatch(r.Context(), resourceType, current.ID, expected)
	}
	return h.cfg.ResourceService.Delete(r.Context(), resourceType, current.ID)
}

func (h *handler) atomicIfMatchService(r *http.Request, resourceType, id string) (ConditionalResourceService, string, error) {
	conditional, ok := h.cfg.ResourceService.(ConditionalResourceService)
	if !ok {
		return nil, "", notImplementedEndpoint("atomic If-Match")
	}

	var current *types.ResourceEnvelope
	if expected, single := singleIfMatchVersion(strings.TrimSpace(r.Header.Get("If-Match"))); !single || expected == "*" {
		var err error
		current, err = h.cfg.ResourceService.Read(r.Context(), resourceType, id)
		if err != nil {
			return nil, "", err
		}
	}
	expected, err := h.resolveIfMatchVersion(r, current)
	if err != nil {
		return nil, "", err
	}
	return conditional, expected, nil
}

func (h *handler) resolveIfMatchVersion(r *http.Request, current *types.ResourceEnvelope) (string, error) {
	if expected, single := singleIfMatchVersion(strings.TrimSpace(r.Header.Get("If-Match"))); single && expected != "*" {
		return expected, nil
	}
	if err := h.checkIfMatch(r, current); err != nil {
		return "", err
	}
	if current == nil || current.VersionID == "" {
		return "", preconditionFailed("If-Match cannot be enforced because the resource has no version")
	}
	return current.VersionID, nil
}

func (h *handler) checkIfMatch(r *http.Request, current *types.ResourceEnvelope) error {
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch == "" || current == nil {
		return nil
	}
	if ifMatchMatches(ifMatch, current.VersionID) {
		return nil
	}
	if current.VersionID != "" {
		return preconditionFailed(fmt.Sprintf("If-Match %q does not match current version %q", ifMatch, current.VersionID))
	}
	return preconditionFailed(fmt.Sprintf("If-Match %q cannot be checked because the resource has no version", ifMatch))
}

func ifMatchMatches(header, currentVersion string) bool {
	if currentVersion == "" {
		return false
	}
	for _, raw := range strings.Split(header, ",") {
		tag := strings.TrimSpace(raw)
		if tag == "*" {
			return true
		}
		if strings.HasPrefix(tag, "W/") {
			tag = strings.TrimSpace(strings.TrimPrefix(tag, "W/"))
		}
		if len(tag) >= 2 && strings.HasPrefix(tag, "\"") && strings.HasSuffix(tag, "\"") {
			tag = tag[1 : len(tag)-1]
		}
		if tag == currentVersion {
			return true
		}
	}
	return false
}

func singleIfMatchVersion(header string) (string, bool) {
	parts := strings.Split(header, ",")
	if len(parts) != 1 {
		return "", false
	}
	tag := strings.TrimSpace(parts[0])
	if tag == "*" {
		return tag, true
	}
	if strings.HasPrefix(tag, "W/") {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "W/"))
	}
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return "", false
	}
	version := tag[1 : len(tag)-1]
	if version == "" {
		return "", false
	}
	return version, true
}
