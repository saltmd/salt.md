package server

import (
	"html"
	"net/http"
	"strings"
)

// Public reading is an ordinary native document flow, independent of the A4
// export/print engine. It includes only fields of the deliberately shared page.
func (s *Server) handlePublicView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	password := ""
	submitted := r.Method == http.MethodPost
	if submitted {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		password = r.PostFormValue("pw")
	}
	id, need, ok, found := s.resolvePublicRequest(r, password)
	nonce := newID()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' https: data:; media-src 'self' https:; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if submitted && !s.formRate.allow(s.clientIP(r)) {
		w.WriteHeader(429)
		w.Write([]byte(s.publicDocument("Try again later", `<h1 class="page-title">Try again later</h1><p>Too many attempts. Please wait a minute.</p>`, nonce)))
		return
	}
	if !found {
		w.WriteHeader(404)
		w.Write([]byte(s.publicDocument("Link unavailable", `<h1 class="page-title">Link unavailable</h1><p>This link may have been disabled or expired.</p>`, nonce)))
		return
	}
	if need && !ok {
		msg := ""
		if submitted {
			w.WriteHeader(403)
			msg = `<p class="public-error" role="alert">Wrong password. Please try again.</p>`
		}
		body := `<h1 class="page-title">Protected page</h1><p>This page is protected by a password.</p>` + msg + `<form class="public-password" method="post" action="/public/` + html.EscapeString(token) + `"><label for="share-password">Page password</label><input id="share-password" type="password" name="pw" autocomplete="current-password" required><button class="btn primary" type="submit">Open page</button></form>`
		w.Write([]byte(s.publicDocument("Protected page", body, nonce)))
		return
	}
	p, err := s.getPage(id)
	if err != nil || p.Trashed {
		w.WriteHeader(404)
		w.Write([]byte(s.publicDocument("Link unavailable", `<h1 class="page-title">Link unavailable</h1>`, nonce)))
		return
	}
	if submitted && need {
		s.setPublicGrant(w, r, token)
	}
	if p.Type == "collection" {
		md, err := s.collectionMarkdown(p)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		w.Write([]byte(s.publicDocument(p.Title, `<pre style="white-space:pre-wrap">`+html.EscapeString(md)+`</pre>`, nonce)))
		return
	}
	p = publicPageCopy(p, token)
	if strings.TrimSpace(p.Title) == "" {
		p.Title = "Untitled"
	}
	var body strings.Builder
	if strings.HasPrefix(p.Cover, "/public/") {
		body.WriteString(`<div class="public-cover"><img src="` + html.EscapeString(p.Cover) + `" alt=""></div>`)
	} else if strings.HasPrefix(p.Cover, "gradient:") && validCover(p.Cover) {
		body.WriteString(`<div class="public-cover" style="background:` + html.EscapeString(strings.TrimPrefix(p.Cover, "gradient:")) + `"></div>`)
	}
	body.WriteString(`<h1 class="page-title">` + s.iconHTML(p.Icon) + html.EscapeString(p.Title) + `</h1>`)
	if p.Description != "" {
		body.WriteString(`<p class="public-description">` + html.EscapeString(p.Description) + `</p>`)
	}
	body.WriteString(publicBlocksHTML(p.Content))
	w.Write([]byte(s.publicDocument(p.Title, body.String(), nonce)))
}

func (s *Server) publicDocument(title, body, nonce string) string {
	css := ""
	if s.publicCSS != "" {
		css = `<link rel="stylesheet" href="` + html.EscapeString(s.publicCSS) + `">`
	}
	script := `<script nonce="` + html.EscapeString(nonce) + `">`
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><meta name="referrer" content="no-referrer"><title>` + html.EscapeString(title) + ` · salt.md</title><link rel="icon" href="/favicon.svg">` + script + publicThemeBoot + `</script>` + css + `<link rel="stylesheet" href="/public-block-colors.css?v=` + html.EscapeString(Version) + `"><style>` + publicViewStyle + `</style><noscript><style>.public-view .theme-switch{display:none}</style></noscript></head><body class="public-view"><header class="public-topbar"><a class="public-brand" href="/" aria-label="salt.md — open workspace"><svg width="26" height="26" viewBox="0 0 64 64" fill="none" aria-hidden="true"><rect x="3" y="3" width="58" height="58" rx="19" fill="currentColor" opacity=".08"/><path d="M42 21c-3-3-6-4-10-4-7 0-12 4-12 9 0 6 6 7 12 8 6 1 11 3 11 8 0 6-5 10-12 10-5 0-10-2-13-6" stroke="currentColor" stroke-width="4" stroke-linecap="round"/><circle cx="47" cy="18" r="3" fill="currentColor"/></svg><span>salt.md</span></a>` + publicThemeSwitch + `</header><main class="public-doc" id="public-document">` + body + `</main><footer class="public-footer">Page shared by link · <a href="/">salt.md</a></footer>` + script + publicThemeControls + `</script></body></html>`
}

const publicThemeSwitch = `<div class="theme-switch" role="radiogroup" aria-label="Appearance"><button type="button" role="radio" aria-checked="false" aria-label="Light theme" title="Light theme" class="theme-switch-opt" data-public-theme="light" tabindex="-1"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></svg></button><button type="button" role="radio" aria-checked="true" aria-label="Automatic — use system theme" title="Automatic — use system theme" class="theme-switch-opt on" data-public-theme="auto" tabindex="0"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M12 17v4m-4 0h8"/></svg></button><button type="button" role="radio" aria-checked="false" aria-label="Dark theme" title="Dark theme" class="theme-switch-opt" data-public-theme="dark" tabindex="-1"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20.9 13a9 9 0 0 1-9.9-9.9A9 9 0 1 0 20.9 13Z"/></svg></button></div>`
const publicThemeBoot = `(()=>{let pref='auto',font='brand';try{const v=localStorage.getItem('salt-theme');if(['light','dark','auto'].includes(v))pref=v;if(localStorage.getItem('salt-font')==='system')font='system';}catch{}const mq=matchMedia('(prefers-color-scheme: dark)');function apply(){document.documentElement.dataset.theme=pref==='auto'?(mq.matches?'dark':'light'):pref;document.documentElement.dataset.font=font;document.querySelectorAll('[data-public-theme]').forEach(b=>{const on=b.dataset.publicTheme===pref;b.classList.toggle('on',on);b.setAttribute('aria-checked',String(on));b.tabIndex=on?0:-1;});}window.saltPublicTheme={apply,set(v){if(!['light','auto','dark'].includes(v))return;pref=v;try{localStorage.setItem('salt-theme',v);}catch{}apply();}};mq.addEventListener('change',apply);addEventListener('storage',e=>{if(e.key==='salt-theme'){pref=['light','dark','auto'].includes(e.newValue)?e.newValue:'auto';apply();}});apply();})();`
const publicThemeControls = `(()=>{const buttons=[...document.querySelectorAll('[data-public-theme]')];buttons.forEach((b,i)=>{b.addEventListener('click',()=>saltPublicTheme.set(b.dataset.publicTheme));b.addEventListener('keydown',e=>{let n=i;if(['ArrowRight','ArrowDown'].includes(e.key))n=(i+1)%buttons.length;else if(['ArrowLeft','ArrowUp'].includes(e.key))n=(i+buttons.length-1)%buttons.length;else if(e.key==='Home')n=0;else if(e.key==='End')n=buttons.length-1;else return;e.preventDefault();buttons[n].focus();saltPublicTheme.set(buttons[n].dataset.publicTheme);});});saltPublicTheme.apply();})();`

// Layout only: all colours, fonts and controls inherit the native app stylesheet.
const publicViewStyle = `.public-view{height:auto;min-height:100vh;margin:0;background:var(--bg);color:var(--fg);font-size:16px;line-height:1.65}.public-topbar{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:16px 24px;border-bottom:1px solid var(--border);background:var(--sidebar-bg)}.public-brand{display:flex;align-items:center;gap:9px;font-weight:600;color:var(--fg);text-decoration:none}.public-doc{max-width:var(--doc-width,940px);margin:0 auto;padding:52px 56px 40px;overflow-wrap:anywhere}.public-doc .page-title{font-size:clamp(28px,4vw,40px);line-height:1.2;margin:0 0 28px}.public-doc .doc-icon{display:inline-block;margin-right:.25em}.public-doc .doc-icon-img,.public-doc .doc-icon-svg{width:1em;height:1em;vertical-align:-.1em}.public-doc h2{font-size:1.5em;line-height:1.3;margin:1.7em 0 .55em}.public-doc h3{font-size:1.2em;margin:1.4em 0 .5em}.public-doc p{margin:.65em 0}.public-description{color:var(--muted);margin-top:-12px!important;margin-bottom:28px!important}.public-doc img{max-width:100%;height:auto;border-radius:var(--radius)}.public-cover{height:clamp(140px,24vw,260px);overflow:hidden;border-radius:var(--radius-lg);margin-bottom:32px}.public-cover img{height:100%;width:100%;object-fit:cover}.public-doc a{color:var(--accent);text-underline-offset:.18em}.public-doc blockquote{border-left:3px solid var(--border-strong);margin:1em 0;padding:.2em 0 .2em 16px;color:var(--muted)}.public-doc pre{background:var(--sidebar-bg);padding:16px;border:1px solid var(--border);border-radius:var(--radius);max-width:100%;overflow:auto;white-space:pre-wrap}.public-doc code{background:var(--hover);padding:2px 5px;border-radius:4px;font-family:var(--mono,monospace);font-size:.9em}.public-doc pre code{background:none;padding:0}.public-doc ul,.public-doc ol{padding-left:1.5em}.public-doc li{margin:.3em 0}.public-doc hr{border:0;border-top:1px solid var(--border);margin:2em 0}.public-callout{display:flex;gap:10px;padding:14px 16px;margin:1em 0;background:var(--hover);border-radius:var(--radius)}.public-table{overflow:auto;max-width:100%;margin:1em 0;border:1px solid var(--border);border-radius:var(--radius)}.public-table table{border-collapse:collapse;width:100%;min-width:420px}.public-table td,.public-table th{padding:9px 13px;text-align:left;border-bottom:1px solid var(--border);border-right:1px solid var(--border)}.public-table tr:first-child{background:var(--hover)}.public-table tr:last-child td{border-bottom:0}.public-table td:last-child{border-right:0}.public-doc .cols{display:grid;gap:24px;grid-template-columns:repeat(2,minmax(0,1fr))}.public-doc .diagram{max-width:100%;overflow:auto}.public-doc .diagram svg{max-width:100%;height:auto}.public-footer{max-width:940px;margin:auto;padding:16px 56px 32px;color:var(--muted);font-size:12px}.public-footer a{color:inherit}.public-password{display:grid;gap:12px;max-width:360px;margin-top:24px}.public-password input{width:100%;padding:10px 12px;background:var(--card);color:var(--fg);border:1px solid var(--border-strong);border-radius:var(--radius);font:inherit}.public-password .btn{justify-self:start}.public-error{color:var(--danger)}.public-view :focus-visible{outline:2px solid var(--accent);outline-offset:3px}@media(max-width:600px){.public-topbar{padding:12px 20px}.public-doc{padding:32px 20px}.public-footer{padding:8px 20px 24px}.public-doc .cols{grid-template-columns:1fr}}@media(prefers-reduced-motion:reduce){.public-view *{transition:none!important;animation:none!important;scroll-behavior:auto!important}}@media print{.public-topbar,.public-footer{display:none}.public-view{background:white;color:black}.public-doc{padding:0;max-width:none}.public-doc img{break-inside:avoid}.public-table{overflow:visible}.public-doc a{color:inherit}}` + publicBlockLayout
