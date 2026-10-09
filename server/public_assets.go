package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A public link grants only the live page and uploads it currently references.
// The derived files index is not authority: an upload can be reused, moved, or
// removed from a block while still present on disk or in the index.
func storedPublicFile(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/files/") {
		return ""
	}
	name := strings.TrimPrefix(u.Path, "/files/")
	if !publicFileName(name) {
		return ""
	}
	return name
}
func publicFileName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\%?#\x00") {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	return filepath.Base(name) == name
}
func publicPageFiles(p *page) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if name := storedPublicFile(raw); name != "" {
			out[name] = true
		}
	}
	add(p.Icon)
	add(p.Cover)
	var content any
	if json.Unmarshal(p.Content, &content) != nil {
		return out
	}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for k, e := range t {
				if k == "url" || k == "href" {
					if raw, ok := e.(string); ok {
						add(raw)
					}
				}
				walk(e)
			}
		}
	}
	walk(content)
	return out
}
func publicAssetURL(token, name string) string {
	return "/public/" + url.PathEscape(token) + "/files/" + url.PathEscape(name)
}
func publicPageCopy(p *page, token string) *page {
	copy := *p
	rewrite := func(raw string) string {
		if name := storedPublicFile(raw); name != "" {
			return publicAssetURL(token, name)
		}
		return raw
	}
	copy.Icon = rewrite(copy.Icon)
	copy.Cover = rewrite(copy.Cover)
	var content any
	if json.Unmarshal(copy.Content, &content) == nil {
		var walk func(any)
		walk = func(v any) {
			switch t := v.(type) {
			case []any:
				for _, e := range t {
					walk(e)
				}
			case map[string]any:
				for k, e := range t {
					if raw, ok := e.(string); ok {
						if k == "url" {
							t[k] = rewrite(raw)
						}
						if k == "href" {
							t[k] = safeURL(rewrite(raw))
						}
					}
					walk(e)
				}
			}
		}
		walk(content)
		if data, err := json.Marshal(content); err == nil {
			copy.Content = data
		}
	}
	return &copy
}

// Password-protected shares use an hour-long, token/path-bound cookie. The
// existing password hash signs it; no password enters an asset URL, no new
// secret or database is needed. Each request still checks the live share.
const publicGrantCookie = "salt-share"

func publicGrantMAC(token, expiry, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte("salt-share-v1:" + token + ":" + expiry))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) publicGrantKey(token string) string {
	var key string
	s.db.QueryRow(`SELECT COALESCE(password_hash,'') FROM share_links WHERE token_hash=? AND mode != 'form'`, tokenHash(token)).Scan(&key)
	return key
}
func (s *Server) setPublicGrant(w http.ResponseWriter, r *http.Request, token string) {
	key := s.publicGrantKey(token)
	if key == "" {
		return
	}
	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	http.SetCookie(w, &http.Cookie{Name: publicGrantCookie, Value: expiry + "." + publicGrantMAC(token, expiry, key), Path: "/public/" + url.PathEscape(token), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: 3600})
}
func (s *Server) hasPublicGrant(r *http.Request, token string) bool {
	c, err := r.Cookie(publicGrantCookie)
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	now := time.Now().Unix()
	if err != nil || exp <= now || exp > now+3600 {
		return false
	}
	key := s.publicGrantKey(token)
	if key == "" {
		return false
	}
	return hmac.Equal([]byte(parts[1]), []byte(publicGrantMAC(token, parts[0], key)))
}
func (s *Server) resolvePublicRequest(r *http.Request, password string) (string, bool, bool, bool) {
	token := r.PathValue("token")
	id, need, ok, found := s.resolveShare(token, password)
	if found && need && !ok {
		ok = s.hasPublicGrant(r, token)
	}
	return id, need, ok, found
}
func (s *Server) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if r.Header.Get("X-Share-Password") != "" && !s.formRate.allow(s.clientIP(r)) {
		http.Error(w, "Try again later", 429)
		return
	}
	id, _, ok, found := s.resolvePublicRequest(r, r.Header.Get("X-Share-Password"))
	name := r.PathValue("name")
	if !found || !ok || !publicFileName(name) {
		http.NotFound(w, r)
		return
	}
	p, err := s.getPage(id)
	if err != nil || p.Trashed || !publicPageFiles(p)[name] {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.dataDir, "files"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind != "" {
		w.Header().Set("Content-Type", kind)
	}
	disposition := "attachment"
	if strings.HasPrefix(kind, "image/") || strings.HasPrefix(kind, "audio/") || strings.HasPrefix(kind, "video/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, st.ModTime(), file)
}

// Read the native CSS href from this binary's Vite build. It stays fingerprinted
// and identical to the app; no second theme or palette is maintained here.
func publicCSSHref(index []byte) string {
	for _, tag := range strings.Split(string(index), "<link") {
		end := strings.Index(tag, ">")
		if end < 0 {
			continue
		}
		tag = tag[:end]
		if !strings.Contains(tag, `rel="stylesheet"`) {
			continue
		}
		start := strings.Index(tag, `href="`)
		if start < 0 {
			continue
		}
		raw := tag[start+6:]
		end = strings.Index(raw, `"`)
		if end < 0 {
			continue
		}
		href := html.UnescapeString(raw[:end])
		if strings.HasPrefix(href, "/assets/") && strings.HasSuffix(href, ".css") && !strings.ContainsAny(href, "<>\"'?#") {
			return href
		}
	}
	return ""
}
