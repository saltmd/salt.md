package server

import (
	"html"
	"io/fs"
	"net/http"
)

// Public share viewer (wave 20): /public/{token} renders the shared page as a
// standalone HTML document — no SPA, no JS required. Previously only the JSON
// API existed and the share URL fell through to the app shell, which showed a
// login screen to anonymous visitors. Password-protected shares render a
// minimal form that POSTs back to the same URL.

// The password prompt for a protected share. notice is "" on the first
// visit, then says why the last attempt did not open the page.
func sharePasswordForm(token string, notice string) string {
	msg := ""
	if notice != "" {
		msg = `<p style="color:#c4554d">` + html.EscapeString(notice) + `</p>`
	}
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>salt.md: protected page</title><style>` + htmlDocStyle + `</style></head><body>` +
		`<h1>🔒 Protected page</h1><p>This page is protected by a password.</p>` + msg +
		`<form id="pwform" method="post" action="/public/` + html.EscapeString(token) + `">` +
		`<input type="password" name="pw" placeholder="Password" autofocus style="padding:9px 11px;border:1px solid #ddd;border-radius:8px;font-size:15px"> ` +
		`<button type="submit" style="padding:9px 14px;border:none;border-radius:8px;background:#2f7d4f;color:#fff;font-size:15px;cursor:pointer">Open</button>` +
		`</form></body></html>`
}

func (s *Server) handlePublicView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	password := ""
	submitted := false
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		r.ParseForm()
		password = r.PostFormValue("pw")
		submitted = true
	} else {
		// A ?pw= address opens a passworded document directly, the same as
		// for collections. A wrong one renders the form with the error, so
		// the gate script records the failure and does not loop it.
		password = publicPassword(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	// Stop sharing has to end the page everywhere at once. Without a word on
	// caching, a proxy or the tunnel's edge may keep a copy of a page whose link
	// was revoked, which is exactly the report in #68.
	w.Header().Set("Cache-Control", "private, no-store")
	// Before the comparison, so a guess past the limit costs nothing and tells
	// nothing, the right password included.
	if password != "" && !s.sharePwRate.allow(sharePwKey(token, s.clientIP(r))) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(sharePasswordForm(token, "Too many attempts. Wait a minute and try again.")))
		return
	}
	pageID, needPW, pwOK, found := s.resolveShare(token, password)
	if !found {
		w.WriteHeader(404)
		w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>salt.md</title><style>` + htmlDocStyle + `</style></head><body><h1>Not found</h1><p>This link is invalid or has expired.</p></body></html>`))
		return
	}
	if needPW && !pwOK {
		// A wrong password redisplays the gate with the error: a POST that
		// failed, or a ?pw= address that did.
		notice := ""
		if submitted || (r.Method != http.MethodPost && password != "") {
			w.WriteHeader(403)
			notice = "Wrong password."
		}
		w.Write([]byte(sharePasswordForm(token, notice)))
		return
	}
	p, err := s.getPage(pageID)
	if err != nil || p.Trashed {
		w.WriteHeader(404)
		w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>salt.md</title><style>` + htmlDocStyle + `</style></head><body><h1>Not found</h1></body></html>`))
		return
	}
	if p.Type == "collection" {
		// Collections render in the SPA (PublicCollection) so the public view
		// looks like the in-app collection: same tabs, same boards/tables.
		// The password (if any) is asked inside the app: the legacy no-JS
		// form below cannot complete the flow for a collection — its POST
		// redirects back to GET and drops the password, so a correct password
		// looped forever. Collections always get the shell, password or not.
		s.servePublicAppShell(w, r)
		return
	}
	opts := publicPrintOptions(s.printOptionsFor(p))
	docCSP(w, &opts)
	w.Write([]byte(s.pageHTML(p, false, opts)))
}

// publicPrintOptions trims the print view down to what an anonymous visitor
// may read. The signed-in print view renders every optional part and hides the
// unchosen ones by class, so its side panel can switch them on without asking
// the server. A visitor has no panel, so a hidden part is only data sitting in
// the source: every comment on the page with its author's name, and the names
// of the workspace and the instance. The names go out only when the house style
// actually shows them, on a cover. Comments never do: a discussion about a page
// is not part of what was published.
func publicPrintOptions(o printOptions) printOptions {
	o.Comments = false
	o.CommentL = nil
	if !o.Cover || !o.Workspace {
		o.WSName, o.Instance = "", ""
	}
	return o
}

// servePublicAppShell serves index.html for the public collection viewer.
func (s *Server) servePublicAppShell(w http.ResponseWriter, r *http.Request) {
	if s.dist == nil {
		httpError(w, 500, "frontend not built")
		return
	}
	b, err := fs.ReadFile(s.dist, "index.html")
	if err != nil {
		httpError(w, 500, "frontend not built")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	// Anonymous responses carry no cacheable identity: nothing here may sit
	// in a shared cache, and a browser must revalidate with the token.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(b)
}

// sharePwKey keys the password limit by link and address together: by address
// alone one guesser would lock everybody behind the same proxy out of every
// protected page, by link alone a stranger could lock the link for everyone.
func sharePwKey(token, ip string) string {
	return tokenHash(token)[:16] + "|" + ip
}
