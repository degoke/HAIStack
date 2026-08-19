package http

import nethttp "net/http"

// WithHealthEndpoints adds lightweight liveness and readiness probes around a
// runtime handler. The readiness callback should report whether the owner has
// completed startup and is still serving.
func WithHealthEndpoints(next nethttp.Handler, ready func() bool) nethttp.Handler {
	mux := nethttp.NewServeMux()
	liveness := func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet && r.Method != nethttp.MethodHead {
			w.Header().Set("Allow", nethttp.MethodGet+", "+nethttp.MethodHead)
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		writeProbe(w, nethttp.StatusOK, "ok\n")
	}
	mux.HandleFunc("/health", liveness)
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet && r.Method != nethttp.MethodHead {
			w.Header().Set("Allow", nethttp.MethodGet+", "+nethttp.MethodHead)
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		if ready != nil && !ready() {
			writeProbe(w, nethttp.StatusServiceUnavailable, "not ready\n")
			return
		}
		writeProbe(w, nethttp.StatusOK, "ready\n")
	})
	if next != nil {
		mux.Handle("/", next)
	}
	return mux
}

func writeProbe(w nethttp.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if status != nethttp.StatusNoContent {
		_, _ = w.Write([]byte(body))
	}
}
