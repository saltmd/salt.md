package server

import (
	"encoding/base64"
	"fmt"
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
	if err != nil {
		return "", err
	}
	defer root.Close()
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
				if !allowed[name] {
					return "", fmt.Errorf("unreferenced upload")
				}
				embedded, ok := cache[name]
				if !ok {
					f, err := root.Open(name)
					if err != nil {
						return "", err
					}
					st, err := f.Stat()
					if err != nil || !st.Mode().IsRegular() || st.Size() > remaining {
						f.Close()
						return "", fmt.Errorf("upload exceeds export limit")
					}
					data, err := io.ReadAll(io.LimitReader(f, remaining+1))
					f.Close()
					if err != nil || int64(len(data)) > remaining {
						return "", fmt.Errorf("upload exceeds export limit")
					}
					mime := http.DetectContentType(data)
					if strings.EqualFold(filepath.Ext(name), ".svg") && strings.Contains(strings.ToLower(string(data)), "<svg") {
						clean := sanitizeSVG(string(data))
						if clean == "" {
							return "", fmt.Errorf("unsafe SVG upload")
						}
						data = []byte(clean)
						mime = "image/svg+xml"
					}
					if !strings.HasPrefix(mime, "image/") {
						return "", fmt.Errorf("upload is not an image")
					}
					sizes[name] = int64(len(data))
					embedded = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
					cache[name] = embedded
				}
				// Count every embedded occurrence, including cached repeats, to bound
				// the resulting document rather than only unique upload reads.
				if sizes[name] > remaining {
					return "", fmt.Errorf("upload exceeds export limit")
				}
				remaining -= sizes[name]
				attr.Val = embedded
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
