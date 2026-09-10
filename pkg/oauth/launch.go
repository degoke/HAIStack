package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// LaunchContext is the SMART EHR launch context returned from /oauth/launch.
type LaunchContext struct {
	PatientID string
	Encounter string
	Intent    string
	NeedPI    bool
}

// LaunchResolver resolves a launch token into SMART launch context.
type LaunchResolver interface {
	ResolveLaunch(ctx context.Context, launch string, iss string) (LaunchContext, error)
}

// LaunchResolverFunc adapts a function to LaunchResolver.
type LaunchResolverFunc func(ctx context.Context, launch string, iss string) (LaunchContext, error)

func (f LaunchResolverFunc) ResolveLaunch(ctx context.Context, launch string, iss string) (LaunchContext, error) {
	return f(ctx, launch, iss)
}

// StaticLaunchResolver returns fixed launch context for demos and tests.
func StaticLaunchResolver(ctx LaunchContext) LaunchResolver {
	return LaunchResolverFunc(func(context.Context, string, string) (LaunchContext, error) {
		return ctx, nil
	})
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.LaunchResolver == nil {
		http.Error(w, "launch not configured", http.StatusNotFound)
		return
	}
	launch := strings.TrimSpace(r.URL.Query().Get("launch"))
	iss := strings.TrimSpace(r.URL.Query().Get("iss"))
	if launch == "" || iss == "" {
		http.Error(w, "launch and iss are required", http.StatusBadRequest)
		return
	}
	ctx, err := s.cfg.LaunchResolver.ResolveLaunch(r.Context(), launch, iss)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"patient":             ctx.PatientID,
		"encounter":           ctx.Encounter,
		"intent":              ctx.Intent,
		"need_patient_banner": ctx.NeedPI,
	})
}

func (s *Server) handleLaunchUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	launch := strings.TrimSpace(r.URL.Query().Get("launch"))
	iss := strings.TrimSpace(r.URL.Query().Get("iss"))
	if launch == "" || iss == "" {
		http.Error(w, "launch and iss are required", http.StatusBadRequest)
		return
	}
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "openid fhirUser launch/patient patient/*.read"
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		state = "launch-state"
	}
	authorizeURL := s.buildAuthorizeURL(launch, iss, clientID, redirectURI, scope, state)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderLaunchPage(launch, iss, authorizeURL)))
}

func (s *Server) buildAuthorizeURL(launch, iss, clientID, redirectURI, scope, state string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"scope":         {scope},
		"state":         {state},
		"launch":        {launch},
		"aud":           {iss},
	}
	return fmt.Sprintf("%s/oauth/authorize?%s", s.cfg.Issuer, q.Encode())
}

func (s *Server) resolveLaunchPatient(ctx context.Context, launch, aud string, patient string) (string, error) {
	if strings.TrimSpace(launch) == "" || s.cfg.LaunchResolver == nil {
		return patient, nil
	}
	iss := aud
	if iss == "" {
		iss = s.cfg.FHIRAudience
	}
	launchCtx, err := s.cfg.LaunchResolver.ResolveLaunch(ctx, launch, iss)
	if err != nil {
		return "", err
	}
	if patient == "" {
		patient = launchCtx.PatientID
	}
	return patient, nil
}

func renderLaunchPage(launch, iss, authorizeURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>SMART EHR Launch</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 640px; margin: 2rem auto; padding: 0 1rem; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 1.5rem; }
    button { background: #0066cc; color: #fff; border: none; padding: 0.75rem 1.5rem; border-radius: 4px; font-size: 1rem; cursor: pointer; }
    button:hover { background: #0052a3; }
    dl { margin: 1rem 0; }
    dt { font-weight: 600; }
    dd { margin: 0 0 0.5rem 0; color: #444; }
  </style>
</head>
<body>
  <div class="card">
    <h1>SMART EHR Launch</h1>
    <p>Review launch context and continue to authorization.</p>
    <dl>
      <dt>Launch token</dt><dd>%s</dd>
      <dt>Issuer (aud)</dt><dd>%s</dd>
    </dl>
    <p><a href="%s"><button type="button">Continue to authorize</button></a></p>
  </div>
</body>
</html>`, htmlEscape(launch), htmlEscape(iss), htmlEscape(authorizeURL))
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(s)
}
