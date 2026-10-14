package server

import (
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
)

// Public share viewer (wave 20): /public/{token} renders the shared page as a
// standalone HTML document — no SPA, no JS required. Previously only the JSON
// API existed and the share URL fell through to the app shell, which showed a
// login screen to anonymous visitors. Password-protected shares render a
// minimal form that POSTs back to the same URL.

// The standalone password gate for server-rendered document shares. It
// mirrors the SPA lock dialog (PublicCollection) so a password prompt looks
// the same on every public link: centered card, lock heading, full-width
// password field and green Open button, with a dark mode. Documents keep the
// working no-JS POST flow — only the look matches the app.
func sharePasswordForm(token string, wrong bool) string {
	msg := ""
	if wrong {
		msg = `<p class="pw-error">Wrong password.</p>`
	}
	wrongJS := "false"
	if wrong {
		wrongJS = "true"
	}
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>salt.md — protected page</title><style>
.pw-page{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px 20px 60px;font:16px/1.6 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1c1a15;background:#fbfaf7}
.pw-card{background:#fff;border:1px solid rgba(28,26,21,.11);border-radius:14px;box-shadow:0 0 0 1px rgba(28,26,21,.07),0 1px 2px rgba(28,26,21,.04),0 22px 48px -24px rgba(28,26,21,.32);padding:32px;width:100%;max-width:400px}
.pw-head{display:flex;align-items:center;gap:12px;margin:0 0 4px}
.pw-head h1{font-size:22px;margin:0;font-weight:700;letter-spacing:-.01em}
.pw-lock{flex:none;width:28px;height:28px;color:#b3123f}
.pw-desc{margin:6px 0 22px;color:#7e7d78;font-size:15px}
.pw-error{color:#c4554d;font-size:14px;margin:0 0 10px}
.pw-input{display:block;width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid rgba(28,26,21,.2);border-radius:8px;font-size:15px;margin-bottom:12px;background:#fff;color:inherit}
.pw-input:focus{outline:none;border-color:#b3123f;box-shadow:0 0 0 3px rgba(179,18,63,.085)}
.pw-btn{display:flex;width:100%;box-sizing:border-box;padding:10px 22px;border:1px solid transparent;border-radius:8px;background:linear-gradient(120deg,#c2185b,#b3123f);color:#fff;font-size:14.5px;font-weight:600;cursor:pointer;justify-content:center;box-shadow:inset 0 0 0 1px rgba(255,255,255,.18),0 2px 10px rgba(179,18,63,.35)}
.pw-btn:hover{filter:brightness(1.05)}
@media (prefers-color-scheme:dark){
.pw-page{color:#d9d9db;background:#0b0b0e}
.pw-card{background:#111116;border-color:rgba(255,255,255,.08)}
.pw-lock{color:#ff5c80}
.pw-desc{color:#808083}
.pw-input{background:#111116;border-color:rgba(255,255,255,.15);color:#d9d9db}
.pw-input:focus{border-color:#ff5c80;box-shadow:0 0 0 3px rgba(255,92,128,.13)}
.pw-btn{background:linear-gradient(120deg,#ff5c80,#ff2d60);box-shadow:inset 0 0 0 1px rgba(255,255,255,.18),0 2px 10px rgba(255,92,128,.35)}
}</style></head><body class="pw-page">` +
		`<div class="pw-card"><div class="pw-head">` +
		`<svg class="pw-lock" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>` +
		`<h1>Protected page</h1></div>` +
		`<p class="pw-desc">This page is protected by a password.</p>` + msg +
		`<form id="pwform" method="post" action="/public/` + html.EscapeString(token) + `">` +
		`<input class="pw-input" type="password" name="pw" placeholder="Password" autofocus> ` +
		`<button class="pw-btn" type="submit">Open</button>` +
		`</form></div><script>(function(){try{` +
		`var m=location.pathname.match(/^\/public\/([a-f0-9]+)$/);if(!m)return;` +
		`var k='salt:share-pw:'+m[1],fk=k+':failed';` +
		`var f=document.getElementById('pwform');if(!f)return;` +
		`var saved=null;try{saved=sessionStorage.getItem(k)}catch(e){}` +
		// A ?pw= address carries its own attempt: consume it like the
		// collection gate does — strip it from the bar, stage it as the
		// session candidate. Optimistic: a wrong one is neutralised by the
		// failure record below, a right one survives reloads.
		`try{var qp=new URLSearchParams(location.search).get('pw');` +
		`if(qp){var u=new URL(location.href);u.searchParams.delete('pw');` +
		`var rs=u.searchParams.toString();history.replaceState(null,'',u.pathname+(rs?'?'+rs:'')+u.hash);` +
		`saved=qp;try{sessionStorage.setItem(k,qp);sessionStorage.removeItem(fk)}catch(e){}}` +
		`}catch(e){}` +
		// A wrong password lands back on this form: remember the failure so
		// the auto-submit below does not loop the same dead password.
		`if(` + wrongJS + `&&saved){try{sessionStorage.setItem(fk,saved)}catch(e){}}` +
		`f.addEventListener('submit',function(){try{sessionStorage.setItem(k,f.pw.value);sessionStorage.removeItem(fk)}catch(e){}});` +
		// Same deal as the collection gate: a password that worked reopens
		// the page straight away, tab-scoped, with no server session.
		`var failed=null;try{failed=sessionStorage.getItem(fk)}catch(e){}` +
		`if(!` + wrongJS + `&&saved&&saved!==failed){f.pw.value=saved;f.submit()}` +
		`}catch(e){}})();</script></body></html>`
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
	if !s.sharePasswordAllowed(r, token, password) {
		httpError(w, 429, "too many attempts. Please try again shortly")
		return
	}
	pageID, needPW, pwOK, found := s.resolveShare(token, password)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	if !found {
		w.WriteHeader(404)
		w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>salt.md</title><style>` + htmlDocStyle + `</style></head><body><h1>Not found</h1><p>This link is invalid or has expired.</p></body></html>`))
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
	if needPW && !pwOK {
		// A wrong password redisplays the gate with the error: a POST that
		// failed, or a ?pw= address that did. The script records the latter
		// so it is not retried in a loop.
		wrong := submitted || (r.Method != http.MethodPost && password != "")
		if wrong {
			w.WriteHeader(403)
		}
		w.Write([]byte(sharePasswordForm(token, wrong)))
		return
	}
	w.Write([]byte(s.pageHTML(p, false, publicPrintOptions(s.printOptionsFor(p)))))
	// A ?pw= address that just opened must not keep the secret in the bar,
	// A ?pw= address that just opened must not keep the secret in the bar,
	// and the tab should remember it like the gates do: the server verified
	// it to render this page, so staging it into session storage is exact —
	// a reload reopens through the gate script without asking again.
	if r.Method != http.MethodPost && password != "" {
		if pwJSON, err := json.Marshal(password); err == nil {
			w.Write([]byte(`<script>(function(){try{` +
				`var m=location.pathname.match(/^\/public\/([a-f0-9]+)$/);if(!m)return;` +
				`var k='salt:share-pw:'+m[1];` +
				`try{sessionStorage.setItem(k,` + string(pwJSON) + `);sessionStorage.removeItem(k+':failed')}catch(e){}` +
				`var u=new URL(location.href);if(u.searchParams.has('pw')){u.searchParams.delete('pw');` +
				`var s=u.searchParams.toString();history.replaceState(null,'',u.pathname+(s?'?'+s:'')+u.hash)}` +
				`}catch(e){}})();</script>`))
		}
	}
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
