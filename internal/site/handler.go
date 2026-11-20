package site

import (
	"errors"
	"mime"
	"net/http"
	"path"
)

// Handler serves the site over HTTP for the local preview. GET and HEAD
// only; every unresolvable request is a plain 404.
func Handler(s Opener) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f, err := s.Open(r.URL.Path)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		ctype := mime.TypeByExtension(path.Ext(f.Name))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "", f.ModTime, f)
	})
}
