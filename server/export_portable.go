package server

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	xhtml "golang.org/x/net/html"
)

// Downloaded documents cannot resolve authenticated root-relative uploads from
// file://. Embed only this page's referenced uploads; never fetch remote URLs.
func (s *Server) portableExportHTML(p *page, r *http.Request) (string, error) {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	origin := &url.URL{Scheme: scheme, Host: r.Host}
	root, err := os.OpenRoot(filepath.Join(s.dataDir, "files"))
	if err == nil {
		defer root.Close()
	}
	allowed := publicPageFiles(p)
	cache := map[string]string{}
	sizes := map[string]int64{}
	remaining := int64(32 << 20)
	z := xhtml.NewTokenizer(strings.NewReader(s.pageHTML(p, false, s.printOptionsFor(p))))
	var out strings.Builder
	for {
		kind := z.Next()
		if kind == xhtml.ErrorToken {
			if z.Err() == io.EOF {
				return out.String(), nil
			}
			return "", z.Err()
		}
		raw := string(z.Raw())
		if kind != xhtml.StartTagToken && kind != xhtml.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		token := z.Token()
		changed := false
		for i := range token.Attr {
			attr := &token.Attr[i]
			name := storedPublicFile(attr.Val)
			if token.Data == "img" && attr.Key == "src" && name != "" {
				embedded, ok := cache[name]
				if !ok && root != nil && allowed[name] {
					data, mime := portableImage(root, name, remaining)
					if data != nil {
						sizes[name] = int64(len(data))
						embedded = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
					}
					cache[name] = embedded
				}
				if embedded != "" && sizes[name] <= remaining {
					remaining -= sizes[name]
					attr.Val = embedded
				} else {
					// Keep the download useful even when an upload cannot be embedded.
					relative, err := url.Parse(attr.Val)
					if err == nil {
						attr.Val = origin.ResolveReference(relative).String()
					}
				}
				changed = true
			} else if attr.Key == "href" && strings.HasPrefix(attr.Val, "/") && !strings.HasPrefix(attr.Val, "//") {
				relative, err := url.Parse(attr.Val)
				if err == nil {
					attr.Val = origin.ResolveReference(relative).String()
					changed = true
				}
			}
		}
		if changed {
			out.WriteString(token.String())
		} else {
			out.WriteString(raw)
		}
	}
}

// OpenRoot keeps failed or escaping uploads outside the exported document.
func portableImage(root *os.Root, name string, remaining int64) ([]byte, string) {
	f, err := root.Open(name)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > remaining {
		return nil, ""
	}
	data, err := io.ReadAll(io.LimitReader(f, remaining+1))
	if err != nil || int64(len(data)) > remaining {
		return nil, ""
	}
	mime := http.DetectContentType(data)
	if strings.EqualFold(filepath.Ext(name), ".svg") {
		clean := sanitizeSVG(string(data))
		if !strings.Contains(clean, "<svg") {
			return nil, ""
		}
		data, mime = []byte(clean), "image/svg+xml"
	}
	if !strings.HasPrefix(mime, "image/") || int64(len(data)) > remaining {
		return nil, ""
	}
	return data, mime
}
