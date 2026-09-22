package http

import (
	"net/http"

	"github.com/degoke/haistack/pkg/smart"
)

func withScopeFilterMatcher(next http.Handler, matcher smart.ScopeFilterMatcher) http.Handler {
	if matcher == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := smart.ContextWithScopeFilterMatcher(r.Context(), matcher)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
