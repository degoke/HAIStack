package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

const (
	defaultSyncPullLimit = 100
	maxSyncPullLimit     = 1000
)

// NewSyncHandler exposes HAIStack sync push/pull endpoints expected by pkg/client.
func NewSyncHandler(hub hasync.HubServer) http.Handler {
	if hub == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, unsupportedEndpoint(r.URL.Path))
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/sync/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r.Method, http.MethodPost)
			return
		}
		body, err := readBody(r)
		if err != nil {
			writeError(w, invalidRequest("read sync push body", err))
			return
		}
		var req struct {
			NodeID   string              `json:"nodeId"`
			TenantID string              `json:"tenantId"`
			Events   []hasync.LocalEvent `json:"events"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, invalidRequest("parse sync push body", err))
			return
		}
		if req.NodeID == "" || req.TenantID == "" {
			writeError(w, invalidRequest("nodeId and tenantId are required", nil))
			return
		}
		var results []hasync.PushResult
		var pushErr error
		if scoped, ok := hub.(hasync.ScopedHubServer); ok {
			results, pushErr = scoped.PushFor(r.Context(), req.NodeID, req.TenantID, req.Events)
		} else {
			for _, event := range req.Events {
				if event.TenantID != req.TenantID || event.OriginNodeID != req.NodeID {
					writeError(w, invalidRequest("sync event tenant or origin node does not match request scope", nil))
					return
				}
			}
			// Legacy hubs do not receive tenant identity as an argument. They are
			// retained for compatibility, but production tenant-aware hubs should
			// implement ScopedHubServer so the store itself enforces the boundary.
			results, pushErr = hub.Push(r.Context(), req.Events)
		}
		if pushErr != nil {
			writeError(w, syncError("sync push failed", pushErr))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
	mux.HandleFunc("/sync/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r.Method, http.MethodGet)
			return
		}
		nodeID := r.URL.Query().Get("nodeId")
		tenantID := r.URL.Query().Get("tenantId")
		if nodeID == "" || tenantID == "" {
			writeError(w, invalidRequest("nodeId and tenantId are required", nil))
			return
		}
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil || after < 0 {
			writeError(w, invalidRequest("after cursor must be a non-negative integer", err))
			return
		}
		limit := defaultSyncPullLimit
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			limit, err = strconv.Atoi(rawLimit)
			if err != nil || limit <= 0 || limit > maxSyncPullLimit {
				writeError(w, invalidRequest("limit must be an integer between 1 and 1000", err))
				return
			}
		}
		var events []hasync.CanonicalEvent
		if scoped, ok := hub.(hasync.ScopedHubServer); ok {
			events, err = scoped.PullFor(r.Context(), nodeID, tenantID, after, limit)
		} else {
			events, err = hub.Pull(r.Context(), after, limit)
			for _, event := range events {
				if event.TenantID != tenantID {
					err = fmt.Errorf("sync hub returned an event outside request tenant scope")
					break
				}
			}
		}
		if err != nil {
			writeError(w, syncError("sync pull failed", err))
			return
		}
		nextCursor := after
		hasMore := false
		if len(events) > 0 {
			nextCursor = events[len(events)-1].CanonicalSequence
			hasMore = limit > 0 && len(events) >= limit
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":     events,
			"nextCursor": nextCursor,
			"hasMore":    hasMore,
		})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/push" && r.URL.Path != "/sync/pull" {
			writeError(w, unsupportedEndpoint(r.URL.Path))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func syncError(message string, err error) error {
	if err == nil {
		return invalidRequest(message, nil)
	}
	return invalidRequest(message, fmt.Errorf("%w", err))
}

// ValidateSyncRequest rejects malformed node and tenant identifiers before a
// caller reaches a legacy hub implementation.
func ValidateSyncRequest(nodeID, tenantID string) error {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(tenantID) == "" {
		return invalidRequest("nodeId and tenantId are required", nil)
	}
	return nil
}

// NewRootHandler combines the FHIR handler with optional sync routes.
func NewRootHandler(fhir http.Handler, sync hasync.HubServer) http.Handler {
	return NewRootHandlerWithSyncMiddleware(fhir, sync, nil)
}

// NewRootHandlerWithSyncMiddleware combines FHIR and sync routes and applies
// the supplied middleware to sync routes as well. This is the recommended
// constructor when FHIR auth is enabled; the two route trees do not otherwise
// share middleware automatically.
func NewRootHandlerWithSyncMiddleware(fhir http.Handler, sync hasync.HubServer, syncMiddleware func(http.Handler) http.Handler) http.Handler {
	return NewRootHandlerFromConfig(RootConfig{FHIR: fhir, Sync: sync, SyncMiddleware: syncMiddleware})
}

// NewRootHandlerFromConfig builds a root handler from RootConfig.
func NewRootHandlerFromConfig(cfg RootConfig) http.Handler {
	mux := http.NewServeMux()
	if cfg.FHIR != nil {
		mux.Handle("/fhir/", cfg.FHIR)
		mux.Handle("/fhir", cfg.FHIR)
	}
	if cfg.Sync != nil {
		syncHandler := NewSyncHandler(cfg.Sync)
		if cfg.SyncMiddleware != nil {
			syncHandler = cfg.SyncMiddleware(syncHandler)
		}
		mux.Handle("/sync/", syncHandler)
	}
	if cfg.FHIR == nil && cfg.Sync == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, unsupportedEndpoint(r.URL.Path))
		})
	}
	return mux
}

// SyncHubServer is an alias for the sync hub server interface used by HTTP wiring.
type SyncHubServer = hasync.HubServer

// RootConfig configures a combined FHIR + sync HTTP handler tree.
type RootConfig struct {
	FHIR http.Handler
	Sync SyncHubServer
	// SyncMiddleware must enforce the caller's node/tenant authentication and
	// authorization when sync routes are exposed.
	SyncMiddleware func(http.Handler) http.Handler
}

// NotImplemented returns an error mapped to HTTP 501 by the HTTP adapter.
func NotImplemented(path string) error {
	return notImplementedEndpoint(path)
}
