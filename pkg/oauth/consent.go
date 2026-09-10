package oauth

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
)

func (s *Server) renderConsent(w http.ResponseWriter, r *http.Request, q url.Values) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	clientID := html.EscapeString(q.Get("client_id"))
	scope := html.EscapeString(q.Get("scope"))
	action := html.EscapeString(r.URL.Path)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorize application</title></head>
<body>
<h1>Authorize application</h1>
<p>Application <strong>%s</strong> is requesting access with scope:</p>
<pre>%s</pre>
<form method="post" action="%s">
%s
<button type="submit" name="approved" value="yes">Allow</button>
<button type="submit" name="approved" value="no">Deny</button>
</form>
</body>
</html>`, clientID, scope, action, consentHiddenFields(q))
}

func consentHiddenFields(q url.Values) string {
	var out string
	for key, values := range q {
		for _, value := range values {
			out += fmt.Sprintf(`<input type="hidden" name="%s" value="%s">`, html.EscapeString(key), html.EscapeString(value))
		}
	}
	return out
}
