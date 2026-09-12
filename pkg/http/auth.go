package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

var errUnauthenticated = errors.New("http: unauthenticated")

type authContextKey struct{}

type requestIdentity struct {
	Principal auth.Principal
	Tenant    auth.TenantContext
}

func withAuth(next http.Handler, resolver PrincipalResolver, checker AuthChecker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		format, err := negotiateResponseFormat(r)
		if err != nil {
			writeError(w, err)
			return
		}
		w = withResponseFormat(w, format)
		principal, tenant, err := resolver(r.Context(), r)
		if err != nil {
			writeError(w, errUnauthenticated)
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, requestIdentity{
			Principal: principal,
			Tenant:    tenant,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func identityFromContext(ctx context.Context) (auth.Principal, auth.TenantContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(requestIdentity)
	if !ok {
		return auth.Principal{}, auth.TenantContext{}, false
	}
	return value.Principal, value.Tenant, true
}

func (h *handler) authorizeRead(ctx context.Context, resourceType, id string) error {
	if h.cfg.AuthChecker == nil || h.cfg.PrincipalResolver == nil {
		return nil
	}
	principal, tenant, ok := identityFromContext(ctx)
	if !ok {
		return errUnauthenticated
	}
	decision, err := h.cfg.AuthChecker.AuthorizeRead(ctx, principal, tenant, resourceType, id)
	if err != nil {
		if errors.Is(err, auth.ErrDenied) {
			return err
		}
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", auth.ErrDenied, decision.Reason)
	}
	return nil
}

func (h *handler) authorizeWrite(ctx context.Context, operation, resourceType, id string) error {
	if h.cfg.AuthChecker == nil || h.cfg.PrincipalResolver == nil {
		return nil
	}
	principal, tenant, ok := identityFromContext(ctx)
	if !ok {
		return errUnauthenticated
	}
	decision, err := h.cfg.AuthChecker.AuthorizeWrite(ctx, principal, tenant, operation, resourceType, id)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", auth.ErrDenied, decision.Reason)
	}
	return nil
}

func (h *handler) authorizeOperation(ctx context.Context, resourceType, operation, id string) error {
	if h.cfg.AuthChecker == nil || h.cfg.PrincipalResolver == nil {
		return nil
	}
	principal, tenant, ok := identityFromContext(ctx)
	if !ok {
		return errUnauthenticated
	}
	if opAuth, ok := h.cfg.AuthChecker.(OperationAuthChecker); ok {
		decision, err := opAuth.AuthorizeOperation(ctx, principal, tenant, resourceType, operation, id)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return fmt.Errorf("%w: %s", auth.ErrDenied, decision.Reason)
		}
		return nil
	}
	switch operation {
	case "$status":
		// Job ownership is verified after the job is loaded; do not authorize on jobId here.
		return nil
	default:
		return h.authorizeWrite(ctx, "operation", resourceType, id)
	}
}

func (h *handler) authorizeJobOwner(ctx context.Context, job *store.JobRecord) error {
	if h.cfg.AuthChecker == nil || h.cfg.PrincipalResolver == nil {
		return nil
	}
	principal, tenant, ok := identityFromContext(ctx)
	if !ok {
		return errUnauthenticated
	}
	owner, recorded := jobs.OwnerFromPayload(job.Payload)
	if !recorded {
		return fmt.Errorf("%w: job owner is not recorded", auth.ErrDenied)
	}
	if owner.PrincipalID != "" && principal.ID != owner.PrincipalID {
		return fmt.Errorf("%w: job access denied", auth.ErrDenied)
	}
	if owner.TenantID != "" && tenant.TenantID != owner.TenantID {
		return fmt.Errorf("%w: job access denied", auth.ErrDenied)
	}
	return nil
}

func (h *handler) authorizeSearch(ctx context.Context, resourceType string) error {
	if h.cfg.AuthChecker == nil || h.cfg.PrincipalResolver == nil {
		return nil
	}
	principal, tenant, ok := identityFromContext(ctx)
	if !ok {
		return errUnauthenticated
	}
	decision, err := h.cfg.AuthChecker.AuthorizeSearch(ctx, principal, tenant, resourceType)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", auth.ErrDenied, decision.Reason)
	}
	return nil
}
