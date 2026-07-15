package http

import (
	"net/http"
)

type handler struct {
	cfg Config
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			writeMethodNotAllowed(w, r.Method)
			return
		}
		h.handleMetadata(w, r)
	case routeTransaction:
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method)
			return
		}
		h.handleTransaction(w, r)
	case routeType:
		switch r.Method {
		case http.MethodGet:
			h.handleSearch(w, r, route.resourceType)
		case http.MethodPost:
			h.handleCreate(w, r, route.resourceType)
		default:
			writeMethodNotAllowed(w, r.Method)
		}
	case routeInstance:
		switch r.Method {
		case http.MethodGet:
			h.handleRead(w, r, route.resourceType, route.id)
		case http.MethodPut:
			h.handleUpdate(w, r, route.resourceType, route.id)
		case http.MethodDelete:
			h.handleDelete(w, r, route.resourceType, route.id)
		default:
			writeMethodNotAllowed(w, r.Method)
		}
	case routeHistory:
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r.Method)
			return
		}
		h.handleHistory(w, r, route.resourceType, route.id)
	default:
		writeError(w, unsupportedEndpoint(r.URL.Path))
	}
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
	writeEnvelope(w, http.StatusOK, envelope, nil)
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
	envelope, err := parseResourceBody(h.cfg.Codec, resourceType, body)
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
	envelope, err := parseResourceBody(h.cfg.Codec, resourceType, body)
	if err != nil {
		writeError(w, err)
		return
	}
	if envelope.ID != "" && envelope.ID != id {
		writeError(w, idMismatch(id, envelope.ID))
		return
	}
	envelope.ID = id
	updated, err := h.cfg.ResourceService.Update(r.Context(), envelope)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, updated, nil)
}

func (h *handler) handleDelete(w http.ResponseWriter, r *http.Request, resourceType, id string) {
	if err := h.authorizeWrite(r.Context(), "delete", resourceType, id); err != nil {
		writeError(w, err)
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
	if err := h.authorizeSearch(r.Context(), resourceType); err != nil {
		writeError(w, err)
		return
	}
	bundle, err := h.cfg.SearchService.SearchBundle(r.Context(), resourceType, r.URL.Query())
	if err != nil {
		writeError(w, err)
		return
	}
	data, err := marshalSearchBundle(bundle)
	if err != nil {
		writeError(w, invalidRequest("build searchset bundle", err))
		return
	}
	writeResource(w, http.StatusOK, data, nil)
}

func (h *handler) handleTransaction(w http.ResponseWriter, r *http.Request) {
	if err := h.authorizeWrite(r.Context(), "transaction", "Bundle", ""); err != nil {
		writeError(w, err)
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	isTxn, err := isTransactionBundle(body)
	if err != nil {
		writeError(w, invalidRequest("parse bundle", err))
		return
	}
	if !isTxn {
		writeError(w, invalidRequest("POST /fhir accepts transaction bundles only", nil))
		return
	}
	envelope, err := parseBundleBody(h.cfg.Codec, body)
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := h.cfg.ResourceService.ProcessTransactionBundle(r.Context(), envelope)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, response, nil)
}
