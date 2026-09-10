package oauth

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) renderConsent(w http.ResponseWriter, r *http.Request, q url.Values, csrf string) {
	title := s.consentUI().Title
	if title == "" {
		title = "Authorize application"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	clientID := html.EscapeString(q.Get("client_id"))
	scope := html.EscapeString(q.Get("scope"))
	action := html.EscapeString(r.URL.Path)
	titleHTML := html.EscapeString(title)
	logo := ""
	if logoURL := strings.TrimSpace(s.consentUI().LogoURL); logoURL != "" {
		logo = fmt.Sprintf(`<p><img src=%q alt="" style="max-height:64px"></p>`, html.EscapeString(logoURL))
	}
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>%s</title></head>
<body>
%s
<h1>%s</h1>
<p>Application <strong>%s</strong> is requesting access with scope:</p>
<pre>%s</pre>
<form method="post" action="%s">
<input type="hidden" name="csrf_token" value="%s">
<button type="submit" name="approved" value="yes">Allow</button>
<button type="submit" name="approved" value="no">Deny</button>
</form>
</body>
</html>`, titleHTML, logo, titleHTML, clientID, scope, action, html.EscapeString(csrf))
}
