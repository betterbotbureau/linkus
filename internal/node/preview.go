package node

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/betterbotbureau/linkus/internal/site"
)

// Preview is the running local preview server.
type Preview struct {
	Addr string // actual listen address
	srv  *http.Server
}

// StartPreview serves the public folder on a loopback address. It returns
// once the listener is accepting.
func StartPreview(addr string, s site.Opener, log *slog.Logger) (*Preview, error) {
	if err := CheckPreviewAddr(addr); err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Handler:           previewHandler(s),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	p := &Preview{Addr: l.Addr().String(), srv: srv}
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("preview server stopped", "error", err)
		}
	}()
	return p, nil
}

// previewRoutes, when another part of the node sets it, answers the
// requests that are not plain file reads: it returns their handler, or nil
// for the folder to answer.
var previewRoutes func(r *http.Request) http.Handler

// Close stops the preview server and releases the folder handle.
func (p *Preview) Close() error {
	err := p.srv.Close()
	return err
}

// previewHandler serves the folder exactly like visitors see it, with one
// owner-only kindness: a bare 404 on the homepage confused owners whose
// index.html sits one level deeper, so the preview explains it. Real
// visitors still get the plain 404 — they learn nothing about the folder.
func previewHandler(s site.Opener) http.Handler {
	h := site.Handler(s)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if previewRoutes != nil {
			if d := previewRoutes(r); d != nil {
				d.ServeHTTP(w, r)
				return
			}
		}
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if f, err := s.Open("/"); err == nil {
				f.Close()
			} else if errors.Is(err, site.ErrNotFound) {
				dir := ""
				if d, ok := s.(interface{ Dir() string }); ok {
					dir = d.Dir()
				}
				serveNoIndexNotice(w, dir)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func serveNoIndexNotice(w http.ResponseWriter, dir string) {
	where := "the folder being served"
	if dir != "" {
		where = "<code>" + html.EscapeString(dir) + "</code>"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>No homepage yet</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5}code{background:#eee;padding:.1rem .3rem;border-radius:4px;overflow-wrap:anywhere}</style>
</head>
<body>
<h1>No homepage yet</h1>
<p>There is no <code>index.html</code> at the top level of %s,
so there is no homepage to show.</p>
<p>If your site lives in a subfolder, point the node at that subfolder
(<code>linknode folder</code>, or on the settings page). Files the folder does contain are still served at their
own paths. Visitors see a plain &ldquo;404 page not found&rdquo; here, not
this note.</p>
</body>
</html>
`, where)
}
