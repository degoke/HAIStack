package http

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type handler struct {
	cfg           Config
	conditionalMu sync.Mutex
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" || r.URL.Path == "/health" {
		h.handleHealth(w, r)
		return
	}
	format, err := negotiateResponseFormat(r)
	if err != nil {
		writeError(w, err)
		return
	}
	w = withResponseFormat(w, format)
	route, err := parseRoute(h.cfg.BasePath, r.URL.Path)
	if err != nil {
		if isPathError(err) {
			writeError(w, invalidRequest(err.Error(), nil))
			return
		}
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}

	switch route.kind {
	case routeMetadata:
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r.Method, http.MethodGet)
			return
		}
		h.handleMetadata(w, r)
	case routeTransaction:
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodPost)
			return
		}
		h.handleBundlePost(w, r)
	case routeSystemSearch:
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodPost)
			return
		}
		h.handleSystemSearch(w, r)
	case routeType:
		h.handleTypeRoute(w, r, route.resourceType)
	case routeTypeSearch:
		h.handleTypeSearch(w, r, route.resourceType)
	case routeInstance:
		h.handleInstanceRoute(w, r, route.resourceType, route.id)
	case routeHistory:
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r.Method, http.MethodGet)
			return
		}
		h.handleHistory(w, r, route.resourceType, route.id)
	case routeOperation:
		if route.operation == "$export" {
			h.handleBulkExport(w, r, route)
			return
		}
		if route.operation == "$validate" && !isSDCResourceOperation(route.operation, route.resourceType) {
			h.handleValidateOperation(w, r, route)
			return
		}
		if route.resourceType == "ImplementationGuide" {
			switch route.operation {
			case "$install":
				h.handleImplementationGuideInstall(w, r, route)
				return
			case "$package":
				writeError(w, notImplementedEndpoint("ImplementationGuide/$package export is not implemented; use CRMI $package semantics"))
				return
			}
		}
		if isSDCOperation(route.operation) && r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodPost)
			return
		}
		if !isSDCOperation(route.operation) && r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
			return
		}
		if isSDCOperation(route.operation) {
			h.handleSDCOperation(w, r, route)
		} else {
			h.handleCustomOperation(w, r, route)
		}
	case routeSystemOperation:
		if route.operation == "$export" {
			h.handleBulkExport(w, r, route)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
			return
		}
		h.handleCustomOperation(w, r, route)
	default:
		writeError(w, unsupportedEndpoint(r.URL.Path))
	}
}

func (h *handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func isSDCOperation(operation string) bool {
	switch operation {
	case "$populate", "$validate", "$extract", "$assemble", "$next-question", "$next", "$answer":
		return true
	default:
		return false
	}
}

func (h *handler) handleCustomOperation(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.OperationService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method == http.MethodGet {
		if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
			writeError(w, err)
			return
		}
	} else if err := h.authorizeWrite(r.Context(), "operation", route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	var body []byte
	var err error
	if r.Method == http.MethodPost {
		body, err = readBodyAllowEmpty(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if len(body) > 0 {
			body, _, err = requestBodyJSON(r.Header.Get("Content-Type"), body)
			if err != nil {
				writeError(w, invalidRequest("parse custom operation input", err))
				return
			}
		}
	}
	result, err := h.cfg.OperationService.Execute(r.Context(), OperationRequest{
		ResourceType: route.resourceType,
		ID:           route.id,
		Operation:    route.operation,
		Query:        r.URL.Query(),
		Body:         body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if result == nil {
		writeError(w, invalidRequest("custom operation returned no resource", nil))
		return
	}
	writeEnvelope(w, http.StatusOK, result, nil)
}

func (h *handler) handleTypeRoute(w http.ResponseWriter, r *http.Request, resourceType string) {
	params, paramsErr := conditionalParamsFromRequest(r)
	if paramsErr != nil {
		writeError(w, paramsErr)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleSearch(w, r, resourceType)
	case http.MethodPost:
		if params != nil {
			if err := h.authorizeWrite(r.Context(), "create", resourceType, ""); err != nil {
				writeError(w, err)
				return
			}
			h.handleConditionalCreate(w, r, resourceType, params)
			return
		}
		h.handleCreate(w, r, resourceType)
	case http.MethodPut:
		if params == nil {
			writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete)
			return
		}
		if err := h.authorizeWrite(r.Context(), "update", resourceType, ""); err != nil {
			writeError(w, err)
			return
		}
		h.handleConditionalUpdate(w, r, resourceType, params)
	case http.MethodDelete:
		if params == nil {
			writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete)
			return
		}
		if err := h.authorizeWrite(r.Context(), "delete", resourceType, ""); err != nil {
			writeError(w, err)
			return
		}
		h.handleConditionalDelete(w, r, resourceType, params)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete)
	}
}

func (h *handler) handleTypeSearch(w http.ResponseWriter, r *http.Request, resourceType string) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		h.handleSearch(w, r, resourceType)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
	}
}

func (h *handler) handleSystemSearch(w http.ResponseWriter, r *http.Request) {
	if h.cfg.SearchService == nil {
		writeError(w, unsupportedEndpoint("_search"))
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	params, err := parseSearchFormBody(body, r.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, invalidRequest("parse search parameters", err))
		return
	}
	typesParam := params.Get("_type")
	parts := strings.Split(typesParam, ",")
	if len(parts) != 1 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, notImplementedEndpoint("multi-resource POST search"))
		return
	}
	params.Del("_type")
	h.handleSearchWithParams(w, r, strings.TrimSpace(parts[0]), params)
}

func (h *handler) handleInstanceRoute(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	switch r.Method {
	case http.MethodGet:
		h.handleRead(w, r, resourceType, id)
	case http.MethodPut:
		h.handleUpdate(w, r, resourceType, id)
	case http.MethodPatch:
		h.handlePatch(w, r, resourceType, id)
	case http.MethodDelete:
		h.handleDelete(w, r, resourceType, id)
	default:
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete)
	}
}

func (h *handler) handleBulkExport(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	writeError(w, notImplementedEndpoint(r.URL.Path))
}

func (h *handler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if h.cfg.CapabilitySource == nil {
		writeError(w, unsupportedEndpoint("/metadata"))
		return
	}
	snapshot := h.cfg.CapabilitySource.CapabilitySnapshot()
	data, err := marshalCapabilityStatement(snapshot, h.cfg.ServerMetadata, h.cfg.SearchService != nil)
	if err != nil {
		writeError(w, invalidRequest("build CapabilityStatement", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func (h *handler) handleRead(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeRead(r.Context(), resourceType, id); err != nil {
		writeError(w, err)
		return
	}
	envelope, err := h.cfg.ResourceService.Read(r.Context(), resourceType, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.enforcePatientScopeOnEnvelope(r.Context(), envelope); err != nil {
		writeError(w, err)
		return
	}
	if envelope == nil {
		writeError(w, invalidRequest("resource service returned no resource", nil))
		return
	}
	summary, elements, err := readProjectionParams(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if ifMatch := strings.TrimSpace(r.Header.Get("If-None-Match")); ifMatch != "" && ifMatchMatches(ifMatch, envelope.VersionID) {
		writeConditionalNotModified(w, envelope)
		return
	}
	if modifiedSince := strings.TrimSpace(r.Header.Get("If-Modified-Since")); modifiedSince != "" && !envelope.LastUpdated.IsZero() {
		if when, parseErr := http.ParseTime(modifiedSince); parseErr == nil && !envelope.LastUpdated.After(when) {
			writeConditionalNotModified(w, envelope)
			return
		}
	}
	projected, err := search.ProjectResource(envelope, summary, elements)
	if err != nil {
		writeError(w, invalidRequest("project resource response", err))
		return
	}
	writeEnvelope(w, http.StatusOK, projected, nil)
}

func readProjectionParams(r *http.Request) (search.SummaryMode, []string, error) {
	values := r.URL.Query()
	summary := search.SummaryNone
	if raw := values.Get("_summary"); raw != "" {
		summary = search.SummaryMode(strings.ToLower(strings.TrimSpace(raw)))
		switch summary {
		case search.SummaryFalse, search.SummaryTrue, search.SummaryText, search.SummaryData:
		default:
			return search.SummaryNone, nil, invalidRequest("unsupported _summary value", nil)
		}
	}
	var elements []string
	for _, raw := range values["_elements"] {
		for _, element := range strings.Split(raw, ",") {
			element = strings.TrimSpace(element)
			if element == "" {
				return search.SummaryNone, nil, invalidRequest("_elements contains an empty element", nil)
			}
			elements = append(elements, element)
		}
	}
	return summary, elements, nil
}

func writeConditionalNotModified(w http.ResponseWriter, envelope *types.ResourceEnvelope) {
	if envelope != nil && envelope.VersionID != "" {
		w.Header().Set("ETag", `W/"`+envelope.VersionID+`"`)
	}
	if envelope != nil && !envelope.LastUpdated.IsZero() {
		w.Header().Set("Last-Modified", envelope.LastUpdated.UTC().Format(http.TimeFormat))
	}
	w.WriteHeader(http.StatusNotModified)
}

func (h *handler) handleCreate(w http.ResponseWriter, r *http.Request, resourceType string) {
	if err := h.authorizeWrite(r.Context(), "create", resourceType, ""); err != nil {
		writeError(w, err)
		return
	}
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
	created, err := h.cfg.ResourceService.Create(r.Context(), envelope)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusCreated, created, resourceHeaders(h.cfg.BasePath, created))
}

func (h *handler) handleUpdate(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeWrite(r.Context(), "update", resourceType, id); err != nil {
		writeError(w, err)
		return
	}
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
	if envelope.ID != "" && envelope.ID != id {
		writeError(w, idMismatch(id, envelope.ID))
		return
	}
	envelope.ID = id

	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" {
		conditional, expected, err := h.atomicIfMatchService(r, resourceType, id)
		if err != nil {
			writeError(w, err)
			return
		}
		updated, err := conditional.UpdateIfMatch(r.Context(), envelope, expected)
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, updated, nil)
		return
	}

	updated, err := h.cfg.ResourceService.Update(r.Context(), envelope)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, updated, nil)
}

func (h *handler) handlePatch(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeWrite(r.Context(), "update", resourceType, id); err != nil {
		writeError(w, err)
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if contentType == "" || (contentType != "application/json-patch+json" && !strings.HasPrefix(contentType, "application/json-patch+json;")) {
		writeError(w, invalidRequest("PATCH requires Content-Type application/json-patch+json", nil))
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" {
		conditional, expected, err := h.atomicIfMatchService(r, resourceType, id)
		if err != nil {
			writeError(w, err)
			return
		}
		patched, err := conditional.PatchIfMatch(r.Context(), resourceType, id, body, expected)
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, patched, nil)
		return
	}
	patched, err := h.cfg.ResourceService.Patch(r.Context(), resourceType, id, body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, patched, nil)
}

func (h *handler) handleDelete(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeWrite(r.Context(), "delete", resourceType, id); err != nil {
		writeError(w, err)
		return
	}
	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" {
		conditional, expected, err := h.atomicIfMatchService(r, resourceType, id)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := conditional.DeleteIfMatch(r.Context(), resourceType, id, expected); err != nil {
			writeError(w, err)
			return
		}
		writeNoContent(w)
		return
	}
	if err := h.cfg.ResourceService.Delete(r.Context(), resourceType, id); err != nil {
		writeError(w, err)
		return
	}
	writeNoContent(w)
}

func (h *handler) handleHistory(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeRead(r.Context(), resourceType, id); err != nil {
		writeError(w, err)
		return
	}
	if h.cfg.PatientReferenceResolver != nil {
		if _, tenant, ok := identityFromContext(r.Context()); ok && tenant.PatientScope != "" {
			current, readErr := h.cfg.ResourceService.Read(r.Context(), resourceType, id)
			if readErr != nil {
				writeError(w, readErr)
				return
			}
			if scopeErr := h.enforcePatientScopeOnEnvelope(r.Context(), current); scopeErr != nil {
				writeError(w, scopeErr)
				return
			}
		}
	}
	versions, err := h.cfg.ResourceService.History(r.Context(), resourceType, id)
	if err != nil {
		writeError(w, err)
		return
	}
	data, err := marshalHistoryBundle(h.cfg.BasePath, resourceType, id, versions)
	if err != nil {
		writeError(w, invalidRequest("build history bundle", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func (h *handler) handleSearch(w http.ResponseWriter, r *http.Request, resourceType string) {
	if h.cfg.SearchService == nil {
		writeError(w, unsupportedEndpoint(resourceType))
		return
	}
	params := r.URL.Query()
	if r.Method == http.MethodPost {
		body, err := readBody(r)
		if err != nil {
			writeError(w, err)
			return
		}
		parsed, err := parseSearchFormBody(body, r.Header.Get("Content-Type"))
		if err != nil {
			writeError(w, invalidRequest("parse search parameters", err))
			return
		}
		params = parsed
	}
	h.handleSearchWithParams(w, r, resourceType, params)
}

func (h *handler) handleSearchWithParams(w http.ResponseWriter, r *http.Request, resourceType string, params url.Values) {
	if h.cfg.SearchService == nil {
		writeError(w, unsupportedEndpoint(resourceType))
		return
	}
	originalParams := params
	params = searchQueryParams(params)
	if err := h.authorizeSearch(r.Context(), resourceType); err != nil {
		writeError(w, err)
		return
	}
	var bundle *search.SearchBundle
	var err error
	if _, tenant, ok := identityFromContext(r.Context()); ok && tenant.PatientScope != "" {
		scoped, ok := h.cfg.SearchService.(PatientScopedSearchService)
		if !ok {
			writeError(w, notImplementedEndpoint("patient-scoped search"))
			return
		}
		bundle, err = scoped.SearchBundleForPatient(r.Context(), resourceType, tenant.PatientScope, params)
	} else {
		bundle, err = h.cfg.SearchService.SearchBundle(r.Context(), resourceType, params)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.filterSearchBundlePatientScope(r.Context(), bundle); err != nil {
		writeError(w, err)
		return
	}
	restoreSearchTransportParams(bundle, originalParams)
	data, err := marshalSearchBundle(bundle)
	if err != nil {
		writeError(w, invalidRequest("build searchset bundle", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func searchQueryParams(params url.Values) url.Values {
	if len(params) == 0 {
		return url.Values{}
	}
	out := make(url.Values, len(params))
	for key, values := range params {
		if key == "_format" || key == "_pretty" {
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func restoreSearchTransportParams(bundle *search.SearchBundle, original url.Values) {
	if bundle == nil || len(bundle.Links) == 0 {
		return
	}
	transport := url.Values{}
	for _, key := range []string{"_format", "_pretty"} {
		for _, value := range original[key] {
			transport.Add(key, value)
		}
	}
	if len(transport) == 0 {
		return
	}
	for relation, rawURL := range bundle.Links {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		query := parsed.Query()
		for key, values := range transport {
			if _, exists := query[key]; !exists {
				query[key] = append([]string(nil), values...)
			}
		}
		parsed.RawQuery = query.Encode()
		bundle.Links[relation] = parsed.String()
	}
}

func (h *handler) handleBundlePost(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	body, _, err = requestBodyJSON(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, invalidRequest("parse bundle body", err))
		return
	}
	isTxn, err := isTransactionBundle(body)
	if err != nil {
		writeError(w, invalidRequest("parse bundle", err))
		return
	}
	if isTxn {
		if err := h.authorizeWrite(r.Context(), "transaction", "Bundle", ""); err != nil {
			writeError(w, err)
			return
		}
		envelope, err := parseBundleBody(h.cfg.Codec, "application/fhir+json", body)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := h.authorizeBundleEntries(r, body); err != nil {
			writeError(w, err)
			return
		}
		response, err := h.cfg.ResourceService.ProcessTransactionBundle(r.Context(), envelope)
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusOK, response, nil)
		return
	}

	isBatch, err := isBatchBundle(body)
	if err != nil {
		writeError(w, invalidRequest("parse bundle", err))
		return
	}
	if !isBatch {
		writeError(w, invalidRequest("POST /fhir accepts transaction or batch bundles", nil))
		return
	}
	if err := h.authorizeWrite(r.Context(), "batch", "Bundle", ""); err != nil {
		writeError(w, err)
		return
	}
	envelope, err := parseBundleBody(h.cfg.Codec, "application/fhir+json", body)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.authorizeBundleEntries(r, body); err != nil {
		writeError(w, err)
		return
	}
	response, err := h.cfg.ResourceService.ProcessBatchBundle(r.Context(), envelope)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, response, nil)
}

// authorizeBundleEntries closes the authorization gap between the bundle
// endpoint and the resources addressed by each entry. Authorizing only the
// Bundle resource would allow a caller with bundle permission to smuggle an
// otherwise denied read or write operation inside the bundle.
func (h *handler) authorizeBundleEntries(r *http.Request, body []byte) error {
	var bundle struct {
		Entry []struct {
			Request struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		return invalidRequest("parse bundle entries", err)
	}
	for _, entry := range bundle.Entry {
		method := strings.ToUpper(strings.TrimSpace(entry.Request.Method))
		path := strings.Trim(strings.TrimSpace(entry.Request.URL), "/")
		if path == "" {
			return invalidRequest("bundle entry request.url is required", nil)
		}
		pathOnly := strings.SplitN(path, "?", 2)[0]
		parts := strings.Split(pathOnly, "/")
		resourceType := parts[0]
		if resourceType == "" || strings.HasPrefix(resourceType, "$") {
			return invalidRequest("bundle entry request.url must address a resource", nil)
		}
		id := ""
		if len(parts) > 2 || (len(parts) == 2 && parts[1] == "") {
			return invalidRequest("bundle entry request.url is invalid", nil)
		}
		if len(parts) == 2 {
			id = parts[1]
		}
		switch method {
		case http.MethodGet:
			if id == "" {
				if err := h.authorizeSearch(r.Context(), resourceType); err != nil {
					return err
				}
			} else if err := h.authorizeRead(r.Context(), resourceType, id); err != nil {
				return err
			}
		case http.MethodPost:
			if err := h.authorizeWrite(r.Context(), "create", resourceType, ""); err != nil {
				return err
			}
		case http.MethodPut:
			if err := h.authorizeWrite(r.Context(), "update", resourceType, id); err != nil {
				return err
			}
		case http.MethodPatch:
			if err := h.authorizeWrite(r.Context(), "update", resourceType, id); err != nil {
				return err
			}
		case http.MethodDelete:
			if err := h.authorizeWrite(r.Context(), "delete", resourceType, id); err != nil {
				return err
			}
		default:
			return invalidRequest("unsupported bundle entry method", nil)
		}
	}
	return nil
}

func parseSearchFormBody(body []byte, contentType string) (url.Values, error) {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		return url.ParseQuery(string(body))
	}
	return nil, invalidRequest("POST search requires application/x-www-form-urlencoded body", nil)
}
