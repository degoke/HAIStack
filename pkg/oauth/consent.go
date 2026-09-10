package oauth

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) renderConsent(w http.ResponseWriter, r *http.Request, q url.Values, csrf string, client Client) {
	ui := s.consentUI()
	title := ui.Title
	if title == "" {
		title = "Authorize application"
	}
	appName := strings.TrimSpace(client.ClientName)
	if appName == "" {
		appName = q.Get("client_id")
	}
	primary := strings.TrimSpace(ui.PrimaryColor)
	if primary == "" {
		primary = defaultConsentPrimaryColor
	}
	pageBG := strings.TrimSpace(ui.PageBackground)
	if pageBG == "" {
		pageBG = defaultConsentPageBackground
	}
	cardBG := strings.TrimSpace(ui.CardBackground)
	if cardBG == "" {
		cardBG = defaultConsentCardBackground
	}
	textColor := strings.TrimSpace(ui.TextColor)
	if textColor == "" {
		textColor = defaultConsentTextColor
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	clientLabel := html.EscapeString(appName)
	scope := html.EscapeString(q.Get("scope"))
	action := html.EscapeString(r.URL.Path)
	titleHTML := html.EscapeString(title)
	logo := ""
	if logoURL := allowedConsentLogoURL(ui.LogoURL); logoURL != "" {
		logo = fmt.Sprintf(`<p class="logo"><img src=%q alt=""></p>`, html.EscapeString(logoURL))
	}
	footer := ""
	if footerText := strings.TrimSpace(ui.FooterText); footerText != "" {
		footer = fmt.Sprintf(`<p class="footer">%s</p>`, html.EscapeString(footerText))
	}
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
:root { --primary:%s; --page-bg:%s; --card-bg:%s; --text:%s; }
body { margin:0; font-family:system-ui,-apple-system,sans-serif; background:var(--page-bg); color:var(--text); }
main { max-width:32rem; margin:3rem auto; padding:0 1rem; }
.card { background:var(--card-bg); border:1px solid #e2e8f0; border-radius:0.75rem; padding:1.5rem; box-shadow:0 1px 2px rgba(15,23,42,.06); }
.logo img { max-height:64px; }
h1 { margin:0 0 1rem; font-size:1.35rem; }
.scope { background:#f1f5f9; border-radius:0.5rem; padding:0.75rem; overflow:auto; font-size:0.9rem; }
.actions { display:flex; gap:0.75rem; margin-top:1.25rem; }
button { cursor:pointer; border-radius:0.5rem; padding:0.55rem 1rem; font-size:0.95rem; border:1px solid transparent; }
button[name="approved"][value="yes"] { background:var(--primary); color:#fff; }
button[name="approved"][value="no"] { background:#fff; color:var(--text); border-color:#cbd5e1; }
.footer { margin-top:1rem; font-size:0.85rem; color:#64748b; }
</style>
</head>
<body>
<main>
<div class="card">
%s
<h1>%s</h1>
<p>Application <strong>%s</strong> is requesting access with scope:</p>
<pre class="scope">%s</pre>
<form method="post" action="%s">
<input type="hidden" name="csrf_token" value="%s">
<div class="actions">
<button type="submit" name="approved" value="yes">Allow</button>
<button type="submit" name="approved" value="no">Deny</button>
</div>
</form>
%s
</div>
</main>
</body>
</html>`, titleHTML, html.EscapeString(primary), html.EscapeString(pageBG), html.EscapeString(cardBG), html.EscapeString(textColor), logo, titleHTML, clientLabel, scope, action, html.EscapeString(csrf), footer)
}

func (s *Server) ensureConsentLogin(w http.ResponseWriter, r *http.Request) (subject string, ok bool) {
	if s.consentLogin == nil {
		return "", true
	}
	return s.consentLogin.EnsureLoggedIn(w, r, r.URL.String())
}

func allowedConsentLogoURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.String()
	default:
		return ""
	}
}
